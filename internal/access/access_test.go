package access

import (
	"context"
	"errors"
	"testing"

	"remna-control-bot/internal/config"
	"remna-control-bot/internal/refcode"
	"remna-control-bot/internal/remnactl"
)

type memState map[int64]bool

func (m memState) SetAwaitingLink(_ context.Context, id int64, on bool) error { m[id] = on; return nil }
func (m memState) AwaitingLink(_ context.Context, id int64) (bool, error)     { return m[id], nil }

func ptr[T any](v T) *T { return &v }

var codec = refcode.New("s")

func setup(mode config.Mode) (*Service, *remnactl.Fake, memState) {
	f := remnactl.NewFake()
	st := memState{}
	return New(f, codec, func() config.Mode { return mode }, st), f, st
}

var ann = User{ID: 100, Name: "Ann"}

func TestStart(t *testing.T) {
	type tc struct {
		name     string
		mode     config.Mode
		seed     []remnactl.Customer
		payload  string
		want     Outcome
		created  bool
		referrer *int
		awaiting bool
	}
	ref := remnactl.Customer{ID: 10, Name: "Ref"}
	archivedRef := remnactl.Customer{ID: 11, Name: "Old", Archived: true}
	member := remnactl.Customer{ID: 20, Name: "Ann", TelegramID: ptr(int64(100))}
	gone := remnactl.Customer{ID: 21, Name: "Ann", TelegramID: ptr(int64(100)), Archived: true}
	for _, c := range []tc{
		{name: "member closed", mode: config.ModeClosed, seed: []remnactl.Customer{member}, want: Member},
		{name: "member ignores ref", mode: config.ModeOpen, seed: []remnactl.Customer{member, ref}, payload: "ref_" + codec.Encode(10), want: Member},
		{name: "archived member", mode: config.ModeOpen, seed: []remnactl.Customer{gone}, want: Archived},
		{name: "closed no payload", mode: config.ModeClosed, want: NeedLink, awaiting: true},
		{name: "open no payload", mode: config.ModeOpen, want: Created, created: true},
		{name: "closed valid ref", mode: config.ModeClosed, seed: []remnactl.Customer{ref}, payload: "ref_" + codec.Encode(10), want: Created, created: true, referrer: ptr(10)},
		{name: "open valid ref", mode: config.ModeOpen, seed: []remnactl.Customer{ref}, payload: "ref_" + codec.Encode(10), want: Created, created: true, referrer: ptr(10)},
		{name: "closed forged ref", mode: config.ModeClosed, seed: []remnactl.Customer{ref}, payload: "ref_zzzzzz", want: BadReferral, awaiting: true},
		{name: "open forged ref", mode: config.ModeOpen, payload: "ref_zzzzzz", want: Created, created: true},
		{name: "closed ref to missing", mode: config.ModeClosed, payload: "ref_" + codec.Encode(999), want: BadReferral, awaiting: true},
		{name: "closed ref to archived", mode: config.ModeClosed, seed: []remnactl.Customer{archivedRef}, payload: "ref_" + codec.Encode(11), want: BadReferral, awaiting: true},
		{name: "open ref to archived", mode: config.ModeOpen, seed: []remnactl.Customer{archivedRef}, payload: "ref_" + codec.Encode(11), want: Created, created: true},
		{name: "closed unknown payload", mode: config.ModeClosed, payload: "hello", want: NeedLink, awaiting: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, st := setup(c.mode)
			for _, cu := range c.seed {
				f.Add(cu)
			}
			r, err := s.Start(context.Background(), ann, c.payload)
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != c.want {
				t.Fatalf("outcome = %v, want %v", r.Outcome, c.want)
			}
			if got := len(f.Creates) == 1; got != c.created {
				t.Fatalf("created = %v, want %v", got, c.created)
			}
			if c.created {
				in := f.Creates[0]
				if *in.TelegramID != 100 || in.Name != "Ann" {
					t.Fatalf("create input %+v", in)
				}
				if (c.referrer == nil) != (in.ReferrerID == nil) || (c.referrer != nil && *c.referrer != *in.ReferrerID) {
					t.Fatalf("referrer = %v, want %v", in.ReferrerID, c.referrer)
				}
				if r.Customer == nil || r.Customer.ID == 0 {
					t.Fatal("created result without customer")
				}
			}
			if st[100] != c.awaiting {
				t.Fatalf("awaiting = %v, want %v", st[100], c.awaiting)
			}
		})
	}
}

func TestStartNameFallback(t *testing.T) {
	s, f, _ := setup(config.ModeOpen)
	if _, err := s.Start(context.Background(), User{ID: 5}, ""); err != nil {
		t.Fatal(err)
	}
	if f.Creates[0].Name != "Telegram 5" {
		t.Fatalf("name = %q", f.Creates[0].Name)
	}
}

func TestStartUnavailable(t *testing.T) {
	s, f, _ := setup(config.ModeOpen)
	f.Err = remnactl.ErrUnavailable
	if _, err := s.Start(context.Background(), ann, ""); !errors.Is(err, remnactl.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func withSub(c remnactl.Customer, short string) remnactl.Customer {
	c.Subscriptions = []remnactl.Subscription{{ID: 1, RwUser: &remnactl.RwUser{ShortUUID: short}}}
	return c
}

func TestLink(t *testing.T) {
	for _, c := range []struct {
		name    string
		seed    remnactl.Customer
		text    string
		want    Outcome
		updates int
	}{
		{"link free", withSub(remnactl.Customer{ID: 1, Name: "X"}, "AbC123xyz"), " https://sub.x/AbC123xyz/ ", Linked, 1},
		{"already mine", withSub(remnactl.Customer{ID: 1, TelegramID: ptr(int64(100))}, "AbC123xyz"), "AbC123xyz", Linked, 0},
		{"someone else's", withSub(remnactl.Customer{ID: 1, TelegramID: ptr(int64(555))}, "AbC123xyz"), "AbC123xyz", Taken, 0},
		{"archived", withSub(remnactl.Customer{ID: 1, Archived: true}, "AbC123xyz"), "AbC123xyz", Archived, 0},
		{"unknown", withSub(remnactl.Customer{ID: 1}, "AbC123xyz"), "Zzz999zzz", NotFound, 0},
		{"garbage", withSub(remnactl.Customer{ID: 1}, "AbC123xyz"), "hi!", BadLink, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, st := setup(config.ModeClosed)
			st[100] = true
			f.Add(c.seed)
			r, err := s.Link(context.Background(), ann, c.text)
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome != c.want || len(f.Updates) != c.updates {
				t.Fatalf("outcome=%v updates=%d", r.Outcome, len(f.Updates))
			}
			if c.want == Linked && (st[100] || r.Customer == nil || *r.Customer.TelegramID != 100) {
				t.Fatalf("linked: awaiting=%v customer=%+v", st[100], r.Customer)
			}
			if c.want != Linked && !st[100] {
				t.Fatal("must keep waiting for a link")
			}
		})
	}
}

func TestLinkKeepsFields(t *testing.T) {
	s, f, _ := setup(config.ModeClosed)
	f.Add(withSub(remnactl.Customer{ID: 1, Name: "X", Contact: "@x", Notes: "vip", ReferrerID: ptr(7), ReferralPercent: ptr(15.0)}, "AbC123xyz"))
	if _, err := s.Link(context.Background(), ann, "AbC123xyz"); err != nil {
		t.Fatal(err)
	}
	in := f.Updates[0].In
	if in.Name != "X" || in.Contact != "@x" || in.Notes != "vip" || *in.ReferrerID != 7 || *in.ReferralPercent != 15 || *in.TelegramID != 100 {
		t.Fatalf("PUT body lost fields: %+v", in)
	}
}
