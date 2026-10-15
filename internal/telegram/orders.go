package telegram

import (
	"context"
	"errors"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/payments"
	"remna-control-bot/internal/telegram/view"
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
	s := view.OrderNotice(b.locFor(ctx, o.ChatID), o, b.links)
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

// onOrderCallback: pc: (cancel) from the customer.
func (b *Bot) onOrderCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { _, _ = b.tg.AnswerCallbackQuery(ctx, answer) }()
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
	for prefix, act := range map[string]func(string) error{
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
