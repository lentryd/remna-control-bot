package telegram

import (
	"context"
	"errors"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/payments"
	"vpn-control-bot/internal/telegram/view"
	"vpn-control-bot/internal/vpnctl"
)

func (b *Bot) PayScreen(ctx context.Context, o orders.Order, details string) error {
	s := view.PayScreen(b.locFor(ctx, o.ChatID), o, details)
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: o.ChatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	return err
}

func (b *Bot) adminCustomer(ctx context.Context, o orders.Order) view.AdminCustomer {
	ac := view.AdminCustomer{TelegramID: o.ChatID}
	if c, err := b.API.GetCustomer(ctx, o.CustomerID); err == nil {
		ac.Name = c.Name
	}
	if ch, err := b.tg.GetChat(ctx, &tgbot.GetChatParams{ChatID: o.ChatID}); err == nil {
		ac.Username = ch.Username
	}
	return ac
}

func (b *Bot) Customer(ctx context.Context, o orders.Order) error {
	s := view.OrderNotice(b.locFor(ctx, o.ChatID), o)
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: o.ChatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	return err
}

// onReceipt takes a receipt when the chat waits for one; false: it doesn't.
func (b *Bot) onReceipt(ctx context.Context, m *models.Message) bool {
	loc := b.loc(ctx)
	if b.Orders == nil {
		return false
	}
	kind, fileID, comment := orders.ReceiptKind(""), "", m.Caption
	switch {
	case len(m.Photo) > 0:
		kind, fileID = orders.ReceiptPhoto, m.Photo[len(m.Photo)-1].FileID
	case m.Document != nil:
		kind, fileID = orders.ReceiptDocument, m.Document.FileID
	case m.Text != "" && !strings.HasPrefix(m.Text, "/"):
		kind, comment = orders.ReceiptText, m.Text
	default:
		kind = "other"
	}
	_, err := b.Orders.AttachReceipt(ctx, m.Chat.ID, kind, fileID, comment)
	switch {
	case errors.Is(err, orders.ErrNotFound):
		return false
	case errors.Is(err, orders.ErrBadRequest):
		b.show(ctx, m.Chat.ID, 0, view.Plain(loc, "msg.receipt_hint"))
	case errors.Is(err, orders.ErrStateChanged):
		b.show(ctx, m.Chat.ID, 0, view.Plain(loc, "msg.order_done"))
	case err != nil:
		b.Log.Error("receipt", "err", err)
		b.show(ctx, m.Chat.ID, 0, view.Plain(loc, "msg.down"))
	default:
		b.show(ctx, m.Chat.ID, 0, view.Plain(loc, "msg.receipt_sent"))
	}
	return true
}

// renewPeriods offers the subscription tariff's periods (or 1/3/6/12 months).
func (b *Bot) renewPeriods(ctx context.Context, chatID int64, msgID int, c *vpnctl.Customer, subID int) {
	loc := b.loc(ctx)
	var sub *vpnctl.Subscription
	for i := range c.Subscriptions {
		if c.Subscriptions[i].ID == subID {
			sub = &c.Subscriptions[i]
		}
	}
	if sub == nil {
		return
	}
	terms := []view.RenewOption{{Months: 1}, {Months: 3}, {Months: 6}, {Months: 12}}
	if ts, err := b.API.ListTariffs(ctx); err == nil && sub.TariffID != nil {
		for _, t := range ts {
			if t.ID == *sub.TariffID && len(t.Periods) > 0 {
				terms = []view.RenewOption{{Months: 1}}
				for _, p := range t.Periods {
					if p.Months == 1 && p.Days == 0 {
						continue
					}
					terms = append(terms, view.RenewOption{Months: p.Months, Days: p.Days})
				}
			}
		}
	}
	var opts []view.RenewOption
	for _, t := range terms {
		q, err := b.Orders.Quote(ctx, c, orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: subID, Months: t.Months, Days: t.Days})
		if err != nil {
			continue
		}
		t.Price = q.Price
		opts = append(opts, t)
	}
	if len(opts) == 0 {
		b.show(ctx, chatID, msgID, view.Notice(loc, loc.T("msg.down")))
		return
	}
	b.show(ctx, chatID, msgID, view.RenewPeriods(loc, subID, sub.Title, opts))
}

const ordersPage = 10

func (b *Bot) showOrders(ctx context.Context, chatID int64, msgID, customerID, page int) {
	loc := b.loc(ctx)
	list, total, err := b.Orders.List(ctx, customerID, ordersPage, page*ordersPage)
	if err == nil && len(list) == 0 && total > 0 && page > 0 { // a stale page past the end
		page = (total - 1) / ordersPage
		list, total, err = b.Orders.List(ctx, customerID, ordersPage, page*ordersPage)
	}
	if err != nil {
		b.Log.Error("orders list", "err", err)
		b.show(ctx, chatID, msgID, view.Notice(loc, loc.T("msg.down")))
		return
	}
	b.show(ctx, chatID, msgID, view.Orders(loc, list, page, total))
}

// onOrderCallback: rp: pr: pb: pc: from the customer.
func (b *Bot) onOrderCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { b.tg.AnswerCallbackQuery(ctx, answer) }()
	if b.Orders == nil {
		answer.Text, answer.ShowAlert = loc.T("msg.soon"), true
		return
	}
	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	r, err := b.Access.Start(ctx, user(&q.From), "")
	if s, done := b.outcome(loc, r, err); done {
		b.show(ctx, chatID, msgID, s)
		return
	}
	c := r.Customer
	if v, ok := parseIDs(q.Data, view.CbRenewPick, 3); ok {
		o, err := b.Orders.Create(ctx, c, chatID, orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: v[0], Months: v[1], Days: v[2]})
		switch {
		case errors.Is(err, orders.ErrManualOff):
			answer.Text, answer.ShowAlert = loc.T("msg.pay_manual_off"), true
		case err != nil:
			b.Log.Error("create order", "err", err)
			answer.Text, answer.ShowAlert = loc.T("msg.down"), true
		default:
			b.show(ctx, chatID, msgID, view.Confirm(loc, *o, b.providerButtons()...))
		}
		return
	}
	if provider, id, ok := parseProviderCallback(q.Data); ok {
		o, err := b.Orders.Pay(ctx, c.ID, id, provider)
		switch {
		case errors.Is(err, orders.ErrStateChanged), errors.Is(err, orders.ErrNotFound), errors.Is(err, orders.ErrBadRequest):
			answer.Text = loc.T("msg.order_done")
		case err != nil:
			b.Log.Error("pay with provider", "provider", provider, "err", err)
			answer.Text, answer.ShowAlert = loc.T("msg.provider_down"), true
		default:
			b.show(ctx, chatID, msgID, view.ProviderPay(loc, *o, b.Orders.Live().Providers.Title(provider)))
		}
		return
	}
	for prefix, act := range map[string]func(string) error{
		view.CbOrderPay: func(id string) error { _, err := b.Orders.RequestReceipt(ctx, c.ID, id); return err },
		view.CbOrderBal: func(id string) error { _, err := b.Orders.PayFromBalance(ctx, c.ID, id); return err },
		view.CbOrderCancel: func(id string) error {
			if err := b.Orders.Cancel(ctx, c.ID, id); err != nil {
				return err
			}
			b.show(ctx, chatID, msgID, view.Plain(loc, "msg.order_cancelled"))
			return nil
		},
	} {
		id, ok := parseOrderID(q.Data, prefix)
		if !ok {
			continue
		}
		err := act(id)
		switch {
		case errors.Is(err, orders.ErrStateChanged), errors.Is(err, orders.ErrNotFound):
			answer.Text = loc.T("msg.order_done")
		case err != nil:
			b.Log.Error("order action", "data", q.Data, "err", err)
			answer.Text, answer.ShowAlert = loc.T("msg.down"), true
		}
		return
	}
}

// InvoiceLink is payments.Stars' LinkFunc: a Telegram Stars invoice link.
func (b *Bot) InvoiceLink(ctx context.Context, title, description, payload string, stars int) (string, error) {
	return b.tg.CreateInvoiceLink(ctx, &tgbot.CreateInvoiceLinkParams{
		Title: title, Description: description, Payload: payload, Currency: "XTR",
		Prices: []models.LabeledPrice{{Label: b.loc(ctx).T("invoice.label"), Amount: stars}},
	})
}

// onPreCheckout answers Telegram's "may this payment go ahead?" within its
// 10 s limit: only for an order of this very user that still waits for it.
func (b *Bot) onPreCheckout(ctx context.Context, q *models.PreCheckoutQuery) {
	loc := b.loc(ctx)
	ans := &tgbot.AnswerPreCheckoutQueryParams{PreCheckoutQueryID: q.ID, OK: true}
	if b.Orders == nil || q.Currency != "XTR" {
		ans.OK, ans.ErrorMessage = false, loc.T("invoice.unavailable")
	} else if err := b.Orders.CheckInvoice(ctx, "stars", q.InvoicePayload, q.From.ID, float64(q.TotalAmount), q.Currency); err != nil {
		ans.OK, ans.ErrorMessage = false, loc.T("invoice.stale")
		b.Log.Warn("pre-checkout refused", "payload", q.InvoicePayload, "user", q.From.ID, "err", err)
	}
	if _, err := b.tg.AnswerPreCheckoutQuery(ctx, ans); err != nil {
		b.Log.Error("answer pre-checkout", "err", err)
	}
}

// onStarsPaid applies a Stars payment; the customer's notice comes from the
// order service. Telegram may repeat the update: a replay is ErrStateChanged.
// This bot only issues XTR invoices, so any other currency is not applied but
// is logged loudly: the customer has paid and an operator must look at it.
func (b *Bot) onStarsPaid(ctx context.Context, m *models.Message) {
	sp := m.SuccessfulPayment
	if b.Orders == nil {
		return
	}
	if sp.Currency != "XTR" {
		b.Log.Error("successful_payment in an unexpected currency, not applied", "currency", sp.Currency,
			"order", sp.InvoicePayload, "charge", sp.TelegramPaymentChargeID, "amount", sp.TotalAmount)
		return
	}
	_, err := b.Orders.Paid(ctx, payments.Event{Provider: "stars", OrderID: sp.InvoicePayload, ExternalID: sp.TelegramPaymentChargeID,
		Paid: true, Amount: float64(sp.TotalAmount), Currency: sp.Currency})
	switch {
	case err == nil, errors.Is(err, orders.ErrStateChanged):
	default:
		// The money is taken but the order was not applied: this needs a human.
		b.Log.Error("stars payment not applied", "order", sp.InvoicePayload, "charge", sp.TelegramPaymentChargeID, "err", err)
	}
}

func (b *Bot) providerButtons() []view.ProviderButton {
	var out []view.ProviderButton
	for _, p := range b.Orders.Live().Providers.Enabled() {
		out = append(out, view.ProviderButton{ID: p.ID(), Title: p.Title()})
	}
	return out
}
