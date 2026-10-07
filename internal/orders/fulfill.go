package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

type Decision string

const (
	DecideFull   Decision = "full"   // received covers it (or no correction)
	DecideDebt   Decision = "debt"   // renew anyway, the balance goes negative
	DecideCredit Decision = "credit" // only put the received money on the balance
)

// checkDecision validates d against the order as it is now; called before and
// after the order is locked so a concurrent correction cannot slip through.
func checkDecision(o *Order, d Decision) error {
	switch {
	case d != DecideFull && d != DecideDebt && d != DecideCredit:
		return bad("decision %q", d)
	case d == DecideDebt && o.Kind != KindRenew:
		return bad("debt only for renewals")
	case d == DecideFull && o.Paid() < o.ToPay:
		return ErrUnderpaid
	}
	return nil
}

func (s *Service) Approve(ctx context.Context, id string, admin string, d Decision) (*Order, error) {
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := checkDecision(o, d); err != nil {
		return nil, err
	}
	if err := s.move(ctx, id, []Status{StatusPendingReview}, StatusApproving,
		map[string]any{"admin": admin, "decision": string(d)}); err != nil {
		return nil, err
	}
	// The snapshot above may predate a correction; decide on what is locked.
	if o, err = s.Get(ctx, id); err != nil {
		return nil, err
	}
	if err := checkDecision(o, d); err != nil {
		if rerr := s.move(ctx, id, []Status{StatusApproving}, StatusPendingReview, nil); rerr != nil {
			s.Log.Error("orders: revert", "order", id, "err", rerr)
		}
		return nil, err
	}
	return s.run(ctx, o)
}

// Retry replays the admin's original decision; only what is missing is redone.
func (s *Service) Retry(ctx context.Context, id string, admin string) (*Order, error) {
	if err := s.move(ctx, id, []Status{StatusFailed}, StatusApproving, map[string]any{"admin": admin, "error": ""}); err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if o.Decision == "" {
		o.Decision = DecideFull
	}
	return s.run(ctx, o)
}

func (s *Service) PayFromBalance(ctx context.Context, customerID int, id string) (*Order, error) {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	if o.Method != MethodBalance {
		return nil, bad("not a balance order")
	}
	if err := s.move(ctx, id, []Status{StatusNew}, StatusApproving, map[string]any{"decision": string(DecideFull)}); err != nil {
		return nil, err
	}
	o.Status, o.Decision = StatusApproving, DecideFull
	return s.run(ctx, o)
}

func (s *Service) SetReceived(ctx context.Context, id string, amount float64, admin string) (*Order, error) {
	if amount <= 0 || amount > 1_000_000 {
		return nil, bad("amount")
	}
	if err := s.move(ctx, id, []Status{StatusPendingReview}, StatusPendingReview,
		map[string]any{"received": round2(amount), "admin": admin}); err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.card(ctx, o)
	return o, nil
}

func (s *Service) Reject(ctx context.Context, id string, reason, admin string) (*Order, error) {
	if err := s.move(ctx, id, []Status{StatusPendingReview}, StatusRejected,
		map[string]any{"reject_reason": strings.TrimSpace(reason), "admin": admin}); err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.card(ctx, o)
	s.customer(ctx, o)
	return o, nil
}

// run applies an approving order. Any failure leaves it `failed` for a retry:
// the panel's payment call is not idempotent, so when it errored without a
// definite answer the money may already be recorded and the admin must check
// before retrying.
func (s *Service) run(ctx context.Context, o *Order) (*Order, error) {
	status, until, ferr := s.fulfill(ctx, o, o.Decision)
	var u any // a typed nil pointer must reach the driver as NULL
	if until != nil {
		u = *until
	}
	set := map[string]any{"panel_payment_id": o.PanelPaymentID, "result_until": u}
	if ferr != nil {
		status = StatusFailed
		set["error"] = failText(o, ferr)
	}
	if err := s.move(ctx, o.ID, []Status{StatusApproving}, status, set); err != nil {
		return nil, err
	}
	got, err := s.Get(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	s.card(ctx, got)
	s.customer(ctx, got)
	return got, nil
}

// definite is a failure the panel reported for sure (nothing was applied).
type definite struct{ msg string }

func (e definite) Error() string { return e.msg }

// failText explains a failure to the admin. Unless the panel gave a definite
// answer (a 4xx or an explicit refusal), the step may have been applied, so
// the admin must check the panel before Retry repeats it.
func failText(o *Order, err error) string {
	var ae *vpnctl.APIError
	var de definite
	if errors.As(err, &de) || (errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500) {
		return errText(err)
	}
	if o.PanelPaymentID == 0 {
		return fmt.Sprintf("результат неизвестен — проверьте в панели платёж «%s» перед повтором: %s", o.Note(), errText(err))
	}
	return fmt.Sprintf("результат неизвестен — проверьте в панели %s (платёж «%s» записан) перед повтором: %s",
		o.target(), o.Note(), errText(err))
}

// target names what the order changes in the panel.
func (o *Order) target() string {
	switch o.Kind {
	case KindSubscribe:
		return fmt.Sprintf("подписки клиента #%d", o.CustomerID)
	case KindAddon:
		return fmt.Sprintf("доп. услуги подписки #%d", o.ItemID)
	case KindChangeTariff:
		return fmt.Sprintf("тариф %s #%d", o.ItemKind, o.ItemID)
	}
	return fmt.Sprintf("срок %s #%d", o.ItemKind, o.ItemID)
}

// staleApproving is how long an approval may run before it is presumed dead.
const staleApproving = 10 * time.Minute

// failStaleApprovals ends approvals interrupted by a crash: the outcome is
// unknown, so they become `failed` with a note to check the panel.
func (s *Service) failStaleApprovals(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM orders WHERE status = ? AND updated_at < ?`,
		StatusApproving, s.Now().Add(-staleApproving))
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		o, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		msg := fmt.Sprintf("результат неизвестен — подтверждение прервалось; проверьте в панели платёж «%s» и %s перед повтором",
			o.Note(), o.target())
		if err := s.move(ctx, id, []Status{StatusApproving}, StatusFailed, map[string]any{"error": msg}); err != nil {
			if errors.Is(err, ErrStateChanged) {
				continue // finished meanwhile
			}
			return err
		}
		if o, err = s.Get(ctx, id); err != nil {
			return err
		}
		s.card(ctx, o)
		s.customer(ctx, o) // the notice says only that it failed; the panel text stays with admins
	}
	return nil
}

func errText(err error) string {
	var ae *vpnctl.APIError
	if errors.As(err, &ae) && ae.Message != "" {
		return ae.Message
	}
	return err.Error()
}

// fulfill is the only place that moves money in the panel. It records the
// payment first (once: PanelPaymentID marks it done) and then applies the
// purchase; o.PanelPaymentID is updated in place.
func (s *Service) fulfill(ctx context.Context, o *Order, d Decision) (Status, *time.Time, error) {
	amount := o.Paid()
	if o.Method == MethodBalance {
		amount = 0
	}
	pay := func(alloc []vpnctl.Allocation) (*vpnctl.PaymentResult, error) {
		res, err := s.API.Pay(ctx, o.CustomerID, vpnctl.PaymentInput{Amount: amount, Method: string(o.Method), Note: o.Note(), Allocations: alloc})
		if err == nil {
			o.PanelPaymentID = res.PaymentID
			// Never forget recorded money, even if the final status write fails.
			if _, uerr := s.DB.ExecContext(ctx, `UPDATE orders SET panel_payment_id = ?, updated_at = ? WHERE id = ?`,
				res.PaymentID, s.Now(), o.ID); uerr != nil {
				s.Log.Error("orders: save payment id", "order", o.ID, "payment", res.PaymentID, "err", uerr)
			}
		}
		return res, err
	}
	if d == DecideCredit || o.Kind == KindTopup {
		if o.PanelPaymentID == 0 {
			if _, err := pay(nil); err != nil {
				return "", nil, err
			}
		}
		if o.Kind == KindTopup {
			return StatusApproved, nil, nil
		}
		return StatusCredited, nil, nil
	}

	if o.Kind == KindRenew {
		if o.PanelPaymentID == 0 && amount > 0 {
			res, err := pay([]vpnctl.Allocation{{Kind: o.ItemKind, ID: o.ItemID, Months: o.Months, Days: o.Days, Amount: o.Price}})
			if err != nil {
				return "", nil, err
			}
			if len(res.Extensions) == 0 || !res.Extensions[0].OK {
				msg := "extension skipped"
				if len(res.Extensions) > 0 {
					msg = res.Extensions[0].Error
				}
				return "", nil, definite{msg}
			}
			return StatusApproved, res.Extensions[0].To, nil
		}
		// Paid from the balance, or a retry after the payment was recorded.
		e, err := s.API.Extend(ctx, o.ItemKind, o.ItemID, vpnctl.ExtendInput{
			Months: o.Months, Days: o.Days, Amount: o.Price, AllowDebt: o.PanelPaymentID != 0,
		})
		if err != nil {
			return "", nil, err
		}
		return StatusApproved, e.To, nil
	}

	if o.PanelPaymentID == 0 && amount > 0 {
		if _, err := pay(nil); err != nil {
			return "", nil, err
		}
	}
	var err error
	switch o.Kind {
	case KindSubscribe:
		_, err = s.API.Provision(ctx, vpnctl.ProvisionInput{
			CustomerID: o.CustomerID, TariffID: o.TariffID, Username: fmt.Sprintf("bot_c%d_o%s", o.CustomerID, o.Short()),
			Months: o.Months, Days: o.Days, Amount: o.Price,
		})
	case KindAddon:
		_, err = s.API.ConnectAddon(ctx, o.ItemID, vpnctl.ConnectAddonInput{TariffID: o.TariffID, Months: o.Months, Days: o.Days, Amount: o.Price})
	case KindChangeTariff:
		err = s.API.ChangeTariff(ctx, o.ItemKind, o.ItemID, vpnctl.ChangeTariffInput{TariffID: o.TariffID, Surcharge: o.Price})
	default:
		err = bad("kind %q", o.Kind)
	}
	if err != nil {
		return "", nil, err
	}
	return StatusApproved, nil, nil
}

// card keeps the admin card current. A failed order without a card (paid
// from the balance) gets one posted so admins see it and can retry.
func (s *Service) card(ctx context.Context, o *Order) {
	if o.AdminMsgID == 0 {
		if o.Status != StatusFailed {
			return
		}
		msgID, err := s.Notify.PostCard(ctx, *o)
		if err != nil {
			s.Log.Error("orders: admin card for failed order", "order", o.ID, "err", err)
			return
		}
		o.AdminMsgID = msgID
		if err := s.set(ctx, o.ID, map[string]any{"admin_msg_id": msgID}); err != nil {
			s.Log.Error("orders: save admin card id", "order", o.ID, "err", err)
		}
		return
	}
	if err := s.Notify.UpdateCard(ctx, *o); err != nil {
		s.Log.Warn("orders: update card", "order", o.ID, "err", err)
	}
}

func (s *Service) customer(ctx context.Context, o *Order) {
	if err := s.Notify.Customer(ctx, *o); err != nil {
		s.Log.Warn("orders: notify customer", "order", o.ID, "err", err)
	}
}
