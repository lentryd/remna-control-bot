package orders

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"vpn-control-bot/internal/payments"
	"vpn-control-bot/internal/vpnctl"
)

// CreateWith stores an order paid with method; empty means the default (see
// Create). A provider method issues its invoice at once; if the provider
// fails, the order is cancelled so no dangling order is left.
func (s *Service) CreateWith(ctx context.Context, c *vpnctl.Customer, chatID int64, r Request, method Method) (*Order, error) {
	q, err := s.Quote(ctx, c, r)
	if err != nil {
		return nil, err
	}
	if len(q.Methods) == 0 {
		return nil, ErrManualOff
	}
	if method == "" {
		if method = q.Methods[0]; method.IsProvider() {
			method = "" // no default provider: the customer picks one
		}
	}
	if method != "" && !slices.Contains(q.Methods, method) {
		return nil, bad("method %q", method)
	}
	r.Amount = 0
	o := &Order{CustomerID: c.ID, ChatID: chatID, Request: r, Title: q.Title, Price: q.Price,
		FromBalance: q.FromBalance, ToPay: q.ToPay, Method: method, Status: StatusNew}
	if method == MethodManual {
		o.Status = StatusAwaitingReceipt
	}
	if err := s.insert(ctx, o); err != nil {
		return nil, err
	}
	if !method.IsProvider() {
		return o, nil
	}
	p, _ := s.Providers.Get(string(method)) // Quote only offers registered providers
	got, err := s.issue(ctx, o, p)
	if err != nil {
		if cerr := s.move(ctx, o.ID, []Status{StatusNew}, StatusCancelled, nil); cerr != nil {
			s.Log.Error("orders: cancel after a failed invoice", "order", o.ID, "err", cerr)
		}
		return nil, err
	}
	return got, nil
}

// Pay issues an invoice with provider for the customer's own order (a new or
// receipt-waiting one) and returns it with PayURL set. Repeating it for the
// same provider returns the same invoice.
func (s *Service) Pay(ctx context.Context, customerID int, id, provider string) (*Order, error) {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	p, ok := s.Providers.Get(provider)
	if !ok || o.ToPay <= 0 {
		return nil, bad("provider %q", provider)
	}
	if o.Status == StatusAwaitingPayment && o.Method == Method(provider) && o.PayURL != "" {
		return o, nil
	}
	if o.Status != StatusNew && o.Status != StatusAwaitingReceipt {
		return nil, ErrStateChanged
	}
	return s.issue(ctx, o, p)
}

// issue bills the order: the invoice is created first; if the order moved on
// meanwhile the invoice is simply left to expire at the provider.
func (s *Service) issue(ctx context.Context, o *Order, p payments.Provider) (*Order, error) {
	if s.CurrencyKnown != nil && !s.CurrencyKnown() {
		return nil, fmt.Errorf("%w: base currency not confirmed by the panel", ErrProvider)
	}
	inv, err := p.Invoice(ctx, payments.InvoiceRequest{OrderID: o.ID, Title: o.Title, Amount: o.ToPay, Currency: s.base(ctx), ExpiresIn: s.TTL})
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrProvider, p.ID(), err)
	}
	if err := s.move(ctx, o.ID, []Status{StatusNew, StatusAwaitingReceipt}, StatusAwaitingPayment, map[string]any{
		"method": p.ID(), "external_id": inv.ExternalID, "pay_url": inv.URL, "pay_amount": inv.Amount, "pay_currency": inv.Currency,
	}); err != nil {
		return nil, err
	}
	if st, ok, _ := s.Chats.GetChatState(ctx, o.ChatID); ok && st.OrderID == o.ID {
		if err := s.Chats.ClearChatState(ctx, o.ChatID); err != nil {
			s.Log.Error("orders: clear chat state", "err", err)
		}
	}
	return s.Get(ctx, o.ID)
}

// paidFrom are the states a provider's confirmation is accepted in: money that
// arrives for an expired or cancelled order is still money.
var paidFrom = []Status{StatusAwaitingPayment, StatusExpired, StatusCancelled}

// Paid applies a provider's statement that an invoice was paid (or looks
// wrong). Replays are harmless: only the first call finds the order in a
// payable state, the rest return ErrStateChanged.
func (s *Service) Paid(ctx context.Context, ev payments.Event) (*Order, error) {
	if !ev.Paid && !ev.Review {
		return nil, nil
	}
	o, err := s.Get(ctx, ev.OrderID)
	if errors.Is(err, ErrNotFound) {
		s.alertUnmatched(ctx, ev)
	}
	if err != nil {
		return nil, err
	}
	if ev.Provider == "" || o.Method != Method(ev.Provider) || (o.ExternalID != "" && ev.ExternalID != "" && o.ExternalID != ev.ExternalID) {
		s.alertUnmatched(ctx, ev)
		return nil, ErrNotFound // somebody else's order or invoice
	}
	set := map[string]any{"external_id": cmp.Or(ev.ExternalID, o.ExternalID)}
	full := ev.Paid && !ev.Review && strings.EqualFold(ev.Currency, o.PayCurrency) && ev.Amount+0.005 >= o.PayAmount
	if full {
		set["admin"], set["decision"] = "provider:"+ev.Provider, string(DecideFull)
		if err := s.move(ctx, o.ID, paidFrom, StatusApproving, set); err != nil {
			return nil, err
		}
		if o, err = s.Get(ctx, o.ID); err != nil {
			return nil, err
		}
		return s.run(ctx, o)
	}

	// Something is off (less money, another currency, the provider's own flag):
	// an admin decides with the usual card.
	set["comment"] = reviewNote(o, ev)
	// Money is comparable only in the currency the order was billed in (the base at issue time, which may differ from
	// the base now); stars are not an amount of the panel's currency.
	if !strings.EqualFold(o.PayCurrency, "XTR") && strings.EqualFold(ev.Currency, o.PayCurrency) && ev.Amount > 0 {
		set["received"] = round2(ev.Amount)
	}
	if err := s.move(ctx, o.ID, paidFrom, StatusPendingReview, set); err != nil {
		return nil, err
	}
	if o, err = s.Get(ctx, o.ID); err != nil {
		return nil, err
	}
	msgID, err := s.Notify.PostCard(ctx, *o)
	if err != nil {
		s.Log.Error("orders: admin card for a provider payment", "order", o.ID, "err", err)
		return o, nil
	}
	o.AdminMsgID = msgID
	if err := s.set(ctx, o.ID, map[string]any{"admin_msg_id": msgID}); err != nil {
		s.Log.Error("orders: save admin card id", "order", o.ID, "err", err)
	}
	return o, nil
}

// alertUnmatched tells the operator about a provider event that is (or may be)
// real money but matches no order: nobody else would ever notice it.
func (s *Service) alertUnmatched(ctx context.Context, ev payments.Event) {
	amount := strconv.FormatFloat(ev.Amount, 'f', -1, 64)
	s.Log.Error("orders: paid event matches no order", "provider", ev.Provider, "order", ev.OrderID,
		"external_id", ev.ExternalID, "amount", amount, "currency", ev.Currency)
	a, ok := s.Notify.(Alerter)
	if !ok {
		return
	}
	text := fmt.Sprintf("⚠️ Платёж провайдера %s не сопоставлен с заказом: заказ %s, счёт %s, сумма %s %s. "+
		"Деньги получены — проверьте платёж у провайдера и вручную примените или верните.",
		ev.Provider, ev.OrderID, ev.ExternalID, amount, ev.Currency)
	if err := a.Alert(ctx, text); err != nil {
		s.Log.Warn("orders: alert the admins", "err", err)
	}
}

func reviewNote(o *Order, ev payments.Event) string {
	billed := strconv.FormatFloat(o.PayAmount, 'f', -1, 64) + " " + o.PayCurrency
	if ev.Amount <= 0 {
		return fmt.Sprintf("%s: провайдер сообщил о неверной сумме (счёт %s)", ev.Provider, billed)
	}
	return fmt.Sprintf("%s: сумма не совпала (счёт %s, получено %s %s)", ev.Provider, billed, strconv.FormatFloat(ev.Amount, 'f', -1, 64), ev.Currency)
}

// CheckInvoice is Telegram's pre-checkout question: may this payment go
// ahead? ErrNotFound hides orders that are not the asker's.
func (s *Service) CheckInvoice(ctx context.Context, provider, id string, chatID int64, amount float64, currency string) error {
	o, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case o.Method != Method(provider) || o.ChatID != chatID:
		return ErrNotFound
	case o.Status != StatusAwaitingPayment:
		return ErrStateChanged
	case !strings.EqualFold(currency, o.PayCurrency) || math.Abs(amount-o.PayAmount) > 0.005:
		return bad("invoice changed")
	}
	return nil
}

// pollAfter: a fresh invoice is left to its webhook for a while.
const pollAfter = 2 * time.Minute

// SweepProviders recovers lost webhooks and ends stale invoices: every
// awaiting_payment order older than pollAfter is asked about at its provider;
// one older than TTL that is still unpaid at that last look expires. A
// cancelled order whose invoice may still be payable (within TTL) is polled
// too, but only to apply a payment — its status never changes otherwise. An
// unreachable provider changes nothing.
func (s *Service) SweepProviders(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM orders WHERE status = ?
		OR (status = ? AND external_id != '' AND created_at > ?) ORDER BY created_at`,
		StatusAwaitingPayment, StatusCancelled, s.Now().Add(-s.TTL))
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() { // collected first: the database has a single connection
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
		if err != nil || (o.Status != StatusAwaitingPayment && o.Status != StatusCancelled) {
			continue
		}
		age := s.Now().Sub(o.CreatedAt)
		if age < pollAfter {
			continue
		}
		applied, err := s.pollPaid(ctx, o)
		if err != nil {
			s.Log.Warn("orders: provider status", "order", o.ID, "provider", o.Method, "err", err)
			continue // unknown: neither apply nor expire
		}
		if applied || o.Status != StatusAwaitingPayment || age < s.TTL {
			continue
		}
		if err := s.move(ctx, o.ID, []Status{StatusAwaitingPayment}, StatusExpired, nil); err != nil && !errors.Is(err, ErrStateChanged) {
			s.Log.Error("orders: expire", "order", o.ID, "err", err)
		}
	}
	return nil
}

// pollPaid asks o's provider whether the invoice was paid and, if it was (or
// looks wrong), applies that through Paid. An order whose provider cannot be
// asked is simply not applied; an error means the answer is unknown.
func (s *Service) pollPaid(ctx context.Context, o *Order) (applied bool, err error) {
	p, ok := s.Providers.Get(string(o.Method))
	if !ok || o.ExternalID == "" {
		return false, nil
	}
	poller, ok := p.(payments.Poller)
	if !ok {
		return false, nil
	}
	ev, err := poller.Status(ctx, o.ExternalID)
	if err != nil {
		return false, err
	}
	ev.Provider, ev.OrderID = string(o.Method), o.ID
	if ev.ExternalID == "" {
		ev.ExternalID = o.ExternalID
	}
	if !ev.Paid && !ev.Review {
		return false, nil
	}
	if _, err := s.Paid(ctx, ev); err != nil && !errors.Is(err, ErrStateChanged) {
		s.Log.Error("orders: apply polled payment", "order", o.ID, "err", err)
	}
	return true, nil
}
