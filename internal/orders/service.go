package orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"vpn-control-bot/internal/billing"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

type Service struct {
	DB     *sql.DB
	Chats  ChatStates
	API    vpnctl.Client
	Notify Notifier
	Now    func() time.Time
	Log    *slog.Logger
	NewID  func() string // order id source; nil: UUIDs (tests use a counter)
	// BaseCurrency is the currency of the panel's prices and balances (ISO
	// code); nil or empty means RUB. Invoices are issued in it.
	BaseCurrency func(context.Context) string
	// CurrencyKnown says whether BaseCurrency is confirmed (by the panel or by the operator) rather than a blind
	// default; provider invoices are refused while it is false. nil: always known.
	CurrencyKnown func() bool

	mu   sync.RWMutex
	live Live
}

// base is the upper-cased base currency, "RUB" when unknown.
func (s *Service) base(ctx context.Context) string {
	if s.BaseCurrency != nil {
		if c := strings.ToUpper(strings.TrimSpace(s.BaseCurrency(ctx))); c != "" {
			return c
		}
	}
	return "RUB"
}

// Quote prices r for c on the server; nothing the client sends but the
// choice itself is trusted.
func (s *Service) Quote(ctx context.Context, c *vpnctl.Customer, r Request) (Quote, error) {
	return s.quote(ctx, s.Live(), c, r)
}

// quote prices r against one Live snapshot, so a caller can use the same values for the rest of its operation.
func (s *Service) quote(ctx context.Context, lv Live, c *vpnctl.Customer, r Request) (Quote, error) {
	q, err := s.price(ctx, c, r)
	if err != nil {
		return Quote{}, err
	}
	if r.Kind != KindTopup {
		sp := billing.Split(q.Price, c.BalanceOwn, c.BalanceReferral, lv.PayMode)
		q.FromBalance, q.ToPay = round2(sp.FromOwn+sp.FromReferral), sp.ToPay
	} else {
		q.ToPay = q.Price
	}
	q.Methods = []Method{}
	switch {
	case q.ToPay == 0:
		q.Methods = []Method{MethodBalance}
	default:
		if lv.Manual {
			q.Methods = append(q.Methods, MethodManual)
		}
		for _, p := range lv.Providers.Enabled() {
			q.Methods = append(q.Methods, Method(p.ID()))
			if q.Titles == nil {
				q.Titles = map[string]string{}
			}
			q.Titles[p.ID()] = p.Title()
		}
	}
	return q, nil
}

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBadRequest}, a...)...)
}

func term(months, days int) string {
	var p []string
	if months > 0 {
		p = append(p, fmt.Sprintf("%d мес.", months))
	}
	if days > 0 {
		p = append(p, fmt.Sprintf("%d дн.", days))
	}
	return strings.Join(p, " ")
}

func (s *Service) price(ctx context.Context, c *vpnctl.Customer, r Request) (Quote, error) {
	if r.Kind != KindTopup && r.Kind != KindChangeTariff && (r.Months < 0 || r.Days < 0 || r.Months+r.Days == 0 || r.Months > 36) {
		return Quote{}, bad("term")
	}
	switch r.Kind {
	case KindTopup:
		p := round2(r.Amount)
		if p <= 0 || p > 1_000_000 {
			return Quote{}, bad("amount")
		}
		return Quote{Title: "Пополнение баланса", Price: p}, nil
	case KindRenew:
		title, ok := itemTitle(c, r.ItemKind, r.ItemID)
		if !ok {
			return Quote{}, bad("item")
		}
		eq, err := s.API.QuoteExtend(ctx, r.ItemKind, r.ItemID, r.Months, r.Days)
		if err != nil {
			return Quote{}, apiErr(err)
		}
		return Quote{Title: fmt.Sprintf("Продление «%s» · %s", title, term(r.Months, r.Days)), Price: eq.Amount}, nil
	case KindSubscribe, KindAddon:
		t, err := s.tariff(ctx, r.TariffID)
		if err != nil {
			return Quote{}, err
		}
		price := TermCost(t.MonthlyPrice, t.Periods, r.Months, r.Days)
		if price <= 0 {
			return Quote{}, bad("free term is not for sale")
		}
		if r.Kind == KindSubscribe {
			if t.Kind != "base" {
				return Quote{}, bad("tariff kind")
			}
			return Quote{Title: fmt.Sprintf("Новая подписка «%s» · %s", t.Name, term(r.Months, r.Days)), Price: price}, nil
		}
		sub, ok := itemTitle(c, "subscription", r.ItemID)
		if t.Kind != "addon" || !ok {
			return Quote{}, bad("addon")
		}
		return Quote{Title: fmt.Sprintf("%s «%s» к «%s» · %s", t.AddonName, t.Name, sub, term(r.Months, r.Days)), Price: price}, nil
	case KindChangeTariff:
		title, ok := itemTitle(c, r.ItemKind, r.ItemID)
		if !ok {
			return Quote{}, bad("item")
		}
		t, err := s.tariff(ctx, r.TariffID)
		if err != nil {
			return Quote{}, err
		}
		tq, err := s.API.QuoteTariff(ctx, r.ItemKind, r.ItemID, r.TariffID)
		if err != nil {
			return Quote{}, apiErr(err)
		}
		return Quote{Title: fmt.Sprintf("Смена тарифа «%s» → «%s»", title, t.Name), Price: math.Max(tq.Surcharge, 0)}, nil
	}
	return Quote{}, bad("kind %q", r.Kind)
}

// apiErr: a 4xx from the panel means the choice is impossible (unlimited
// subscription, inactive tariff…); anything else is passed on.
func apiErr(err error) error {
	var ae *vpnctl.APIError
	if errors.As(err, &ae) && ae.Status < 500 {
		return bad("%s", ae.Message)
	}
	return err
}

func (s *Service) tariff(ctx context.Context, id int) (*vpnctl.Tariff, error) {
	ts, err := s.API.ListTariffs(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.ID == id && t.Active {
			return &t, nil
		}
	}
	return nil, bad("tariff %d", id)
}

// itemTitle finds the customer's own subscription or add-on.
func itemTitle(c *vpnctl.Customer, kind string, id int) (string, bool) {
	for _, s := range c.Subscriptions {
		if kind == "subscription" && s.ID == id {
			return s.Title, true
		}
		for _, a := range s.Addons {
			if kind == "addon" && a.ID == id {
				return a.AddonName, true
			}
		}
	}
	return "", false
}

// Create is CreateWith the default method: the balance when it covers the
// whole price, manual payment when it is on, otherwise an undecided order the
// customer pays with Pay.
func (s *Service) Create(ctx context.Context, c *vpnctl.Customer, chatID int64, r Request) (*Order, error) {
	return s.CreateWith(ctx, c, chatID, r, "")
}

// RequestReceipt makes the customer's chat wait for this order's receipt
// and shows the amount with the payment details there.
func (s *Service) RequestReceipt(ctx context.Context, customerID int, id string) (*Order, error) {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return nil, err
	}
	if o.Status != StatusAwaitingReceipt {
		return nil, ErrStateChanged
	}
	if err := s.Chats.SetChatState(ctx, o.ChatID, store.ChatState{Kind: "receipt", OrderID: o.ID}); err != nil {
		return nil, err
	}
	return o, s.Notify.PayScreen(ctx, *o, s.Live().Details)
}

// AttachReceipt takes the receipt the chat was asked for and hands the
// order to the admins. ErrNotFound: the chat waits for nothing.
func (s *Service) AttachReceipt(ctx context.Context, chatID int64, kind ReceiptKind, fileID, comment string) (*Order, error) {
	st, ok, err := s.Chats.GetChatState(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if !ok || st.Kind != "receipt" {
		return nil, ErrNotFound
	}
	if kind != ReceiptPhoto && kind != ReceiptDocument && kind != ReceiptText {
		return nil, bad("receipt kind %q", kind)
	}
	if len([]rune(comment)) > 500 {
		comment = string([]rune(comment)[:500])
	}
	err = s.move(ctx, st.OrderID, []Status{StatusAwaitingReceipt}, StatusPendingReview,
		map[string]any{"receipt_kind": kind, "receipt_file_id": fileID, "comment": comment})
	if cerr := s.Chats.ClearChatState(ctx, chatID); cerr != nil {
		s.Log.Error("orders: clear chat state", "err", cerr)
	}
	if err != nil {
		return nil, err
	}
	o, err := s.Get(ctx, st.OrderID)
	if err != nil {
		return nil, err
	}
	msgID, err := s.Notify.PostCard(ctx, *o)
	if err != nil {
		s.Log.Error("orders: admin card", "order", o.ID, "err", err)
		return o, nil // the order is safe; admins see it in the next card update or the logs
	}
	o.AdminMsgID = msgID
	if err := s.set(ctx, o.ID, map[string]any{"admin_msg_id": msgID}); err != nil {
		s.Log.Error("orders: save admin card id", "order", o.ID, "err", err)
	}
	return o, nil
}

func (s *Service) Cancel(ctx context.Context, customerID int, id string) error {
	o, err := s.ForCustomer(ctx, customerID, id)
	if err != nil {
		return err
	}
	if err := s.move(ctx, o.ID, []Status{StatusNew, StatusAwaitingReceipt, StatusAwaitingPayment}, StatusCancelled, nil); err != nil {
		return err
	}
	if st, ok, _ := s.Chats.GetChatState(ctx, o.ChatID); ok && st.OrderID == o.ID {
		return s.Chats.ClearChatState(ctx, o.ChatID)
	}
	return nil
}

// ExpireStale ends orders nobody sent a receipt for within TTL and fails
// approvals that were interrupted.
func (s *Service) ExpireStale(ctx context.Context) error {
	if err := s.failStaleApprovals(ctx); err != nil {
		s.Log.Error("orders: stale approvals", "err", err)
	}
	ttl := s.Live().TTL
	_, err := s.DB.ExecContext(ctx, `UPDATE orders SET status = ?, updated_at = ? WHERE status IN (?, ?) AND created_at < ?`,
		StatusExpired, s.Now(), StatusNew, StatusAwaitingReceipt, s.Now().Add(-ttl))
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM chat_state WHERE kind = 'receipt' AND order_id IN (SELECT id FROM orders WHERE status = ?)`, StatusExpired)
	return err
}
