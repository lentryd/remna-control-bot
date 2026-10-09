// Package access decides who may use the bot and turns a Telegram user
// into a vpn-control customer (spec: "Access and onboarding").
package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/sublink"
	"vpn-control-bot/internal/vpnctl"
)

type User struct {
	ID   int64
	Name string
}

type Outcome int

const (
	Member      Outcome = iota // known, active customer
	Created                    // new customer created in vpn-control
	NeedLink                   // closed mode: send your subscription link
	BadReferral                // closed mode: the referral link is invalid
	Archived                   // customer exists but is archived
	Linked                     // subscription link accepted, telegram id set
	BadLink                    // text is not a subscription link
	NotFound                   // no subscription with this link
	Taken                      // subscription belongs to another Telegram account
)

type Result struct {
	Outcome  Outcome
	Customer *vpnctl.Customer
}

type State interface {
	SetAwaitingLink(ctx context.Context, tgID int64, on bool) error
	AwaitingLink(ctx context.Context, tgID int64) (bool, error)
}

type Service struct {
	api   vpnctl.Client
	codec *refcode.Codec
	mode  func() config.Mode
	state State
}

func New(api vpnctl.Client, codec *refcode.Codec, mode func() config.Mode, state State) *Service {
	return &Service{api: api, codec: codec, mode: mode, state: state}
}

func (s *Service) AwaitingLink(ctx context.Context, tgID int64) (bool, error) {
	return s.state.AwaitingLink(ctx, tgID)
}

func (s *Service) Start(ctx context.Context, u User, payload string) (Result, error) {
	c, err := s.api.LookupByTelegram(ctx, u.ID)
	if err == nil {
		if c.Archived {
			return Result{Outcome: Archived, Customer: c}, nil
		}
		return Result{Outcome: Member, Customer: c}, nil
	}
	if !errors.Is(err, vpnctl.ErrNotFound) {
		return Result{}, err
	}

	var referrer *int
	if code, ok := strings.CutPrefix(strings.TrimSpace(payload), "ref_"); ok {
		id, ok, err := s.referrer(ctx, code)
		if err != nil {
			return Result{}, err
		}
		if ok {
			referrer = &id
		} else if s.mode() == config.ModeClosed {
			return Result{Outcome: BadReferral}, s.state.SetAwaitingLink(ctx, u.ID, true)
		}
	}
	if referrer == nil && s.mode() == config.ModeClosed {
		return Result{Outcome: NeedLink}, s.state.SetAwaitingLink(ctx, u.ID, true)
	}
	return s.create(ctx, u, referrer)
}

// referrer resolves a referral code to an active customer id.
func (s *Service) referrer(ctx context.Context, code string) (int, bool, error) {
	id, ok := s.codec.Decode(code)
	if !ok {
		return 0, false, nil
	}
	c, err := s.api.GetCustomer(ctx, id)
	if errors.Is(err, vpnctl.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, !c.Archived, nil
}

func (s *Service) create(ctx context.Context, u User, referrer *int) (Result, error) {
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = fmt.Sprintf("Telegram %d", u.ID)
	}
	tg := u.ID
	id, err := s.api.CreateCustomer(ctx, vpnctl.CustomerInput{Name: name, TelegramID: &tg, ReferrerID: referrer})
	if err != nil {
		return Result{}, err
	}
	c, err := s.api.GetCustomer(ctx, id)
	if err != nil {
		return Result{}, err
	}
	return Result{Outcome: Created, Customer: c}, s.state.SetAwaitingLink(ctx, u.ID, false)
}

func (s *Service) Link(ctx context.Context, u User, text string) (Result, error) {
	short, ok := sublink.Parse(text)
	if !ok {
		return Result{Outcome: BadLink}, nil
	}
	c, err := s.api.LookupByShortUUID(ctx, short)
	if errors.Is(err, vpnctl.ErrNotFound) {
		return Result{Outcome: NotFound}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if c.TelegramID != nil && *c.TelegramID != u.ID {
		return Result{Outcome: Taken}, nil
	}
	if c.Archived {
		return Result{Outcome: Archived, Customer: c}, nil
	}
	if c.TelegramID == nil {
		// Full PUT: start from the current customer so nothing else is cleared.
		in := c.Input()
		tg := u.ID
		in.TelegramID = &tg
		if err := s.api.UpdateCustomer(ctx, c.ID, in); err != nil {
			return Result{}, err
		}
		c.TelegramID = &tg
	}
	return Result{Outcome: Linked, Customer: c}, s.state.SetAwaitingLink(ctx, u.ID, false)
}
