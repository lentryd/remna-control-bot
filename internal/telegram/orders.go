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

var (
	_ orders.Notifier = (*Bot)(nil)
	_ orders.Alerter  = (*Bot)(nil)
)

// Alert sends a plain message to the admin chat (no parse mode: the text may
// hold any characters). Without an admin chat it does nothing.
func (b *Bot) Alert(ctx context.Context, text string) error {
	if b.AdminChatID == 0 {
		return nil
	}
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: b.AdminChatID, Text: text})
	return err
}

func (b *Bot) PayScreen(ctx context.Context, o orders.Order, details string) error {
	s := view.PayScreen(o, details)
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

// PostCard re-sends the receipt into the admin chat with the order card.
func (b *Bot) PostCard(ctx context.Context, o orders.Order) (int, error) {
	s := view.AdminCard(o, b.adminCustomer(ctx, o))
	var markup models.ReplyMarkup
	if s.Keyboard != nil {
		markup = s.Keyboard
	}
	var m *models.Message
	var err error
	switch o.ReceiptKind {
	case orders.ReceiptPhoto:
		m, err = b.tg.SendPhoto(ctx, &tgbot.SendPhotoParams{ChatID: b.AdminChatID, Photo: &models.InputFileString{Data: o.ReceiptFileID},
			Caption: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	case orders.ReceiptDocument:
		m, err = b.tg.SendDocument(ctx, &tgbot.SendDocumentParams{ChatID: b.AdminChatID, Document: &models.InputFileString{Data: o.ReceiptFileID},
			Caption: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	default:
		m, err = b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: b.AdminChatID,
			Text: cardText(s.Text, o), ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	}
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}

func (b *Bot) UpdateCard(ctx context.Context, o orders.Order) error {
	s := view.AdminCard(o, b.adminCustomer(ctx, o))
	markup := s.Keyboard
	if markup == nil {
		markup = &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
	}
	var err error
	// Text receipts and balance orders (no receipt) have a text card.
	if o.ReceiptKind != orders.ReceiptPhoto && o.ReceiptKind != orders.ReceiptDocument {
		_, err = b.tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: b.AdminChatID, MessageID: o.AdminMsgID,
			Text: cardText(s.Text, o), ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	} else {
		_, err = b.tg.EditMessageCaption(ctx, &tgbot.EditMessageCaptionParams{ChatID: b.AdminChatID, MessageID: o.AdminMsgID,
			Caption: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	}
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

// cardText is a text card: the card plus the customer's comment, if any.
func cardText(text string, o orders.Order) string {
	if o.Comment == "" {
		return text
	}
	return text + "\n\n📝 " + view.Esc(o.Comment)
}

func (b *Bot) Customer(ctx context.Context, o orders.Order) error {
	s := view.OrderNotice(o)
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: o.ChatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	return err
}

// onReceipt takes a receipt when the chat waits for one; false: it doesn't.
func (b *Bot) onReceipt(ctx context.Context, m *models.Message) bool {
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
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtReceiptHint))
	case errors.Is(err, orders.ErrStateChanged):
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtOrderDone))
	case err != nil:
		b.Log.Error("receipt", "err", err)
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtDown))
	default:
		b.show(ctx, m.Chat.ID, 0, view.Plain(view.TxtReceiptSent))
	}
	return true
}

// renewPeriods offers the subscription tariff's periods (or 1/3/6/12 months).
func (b *Bot) renewPeriods(ctx context.Context, chatID int64, msgID int, c *vpnctl.Customer, subID int) {
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
		b.show(ctx, chatID, msgID, view.Notice(view.TxtDown))
		return
	}
	b.show(ctx, chatID, msgID, view.RenewPeriods(subID, sub.Title, opts))
}

const ordersPage = 10

func (b *Bot) showOrders(ctx context.Context, chatID int64, msgID, customerID, page int) {
	list, total, err := b.Orders.List(ctx, customerID, ordersPage, page*ordersPage)
	if err == nil && len(list) == 0 && total > 0 && page > 0 { // a stale page past the end
		page = (total - 1) / ordersPage
		list, total, err = b.Orders.List(ctx, customerID, ordersPage, page*ordersPage)
	}
	if err != nil {
		b.Log.Error("orders list", "err", err)
		b.show(ctx, chatID, msgID, view.Notice(view.TxtDown))
		return
	}
	b.show(ctx, chatID, msgID, view.Orders(list, page, total))
}

// onOrderCallback: rp: pr: pb: pc: from the customer.
func (b *Bot) onOrderCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { b.tg.AnswerCallbackQuery(ctx, answer) }()
	if b.Orders == nil {
		answer.Text, answer.ShowAlert = view.TxtSoon, true
		return
	}
	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	r, err := b.Access.Start(ctx, user(&q.From), "")
	if s, done := b.outcome(r, err); done {
		b.show(ctx, chatID, msgID, s)
		return
	}
	c := r.Customer
	if v, ok := parseIDs(q.Data, view.CbRenewPick, 3); ok {
		o, err := b.Orders.Create(ctx, c, chatID, orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: v[0], Months: v[1], Days: v[2]})
		switch {
		case errors.Is(err, orders.ErrManualOff):
			answer.Text, answer.ShowAlert = view.TxtPayManualOff, true
		case err != nil:
			b.Log.Error("create order", "err", err)
			answer.Text, answer.ShowAlert = view.TxtDown, true
		default:
			b.show(ctx, chatID, msgID, view.Confirm(*o, b.providerButtons()...))
		}
		return
	}
	if provider, id, ok := parseProviderCallback(q.Data); ok {
		o, err := b.Orders.Pay(ctx, c.ID, id, provider)
		switch {
		case errors.Is(err, orders.ErrStateChanged), errors.Is(err, orders.ErrNotFound), errors.Is(err, orders.ErrBadRequest):
			answer.Text = view.TxtOrderDone
		case err != nil:
			b.Log.Error("pay with provider", "provider", provider, "err", err)
			answer.Text, answer.ShowAlert = view.TxtProviderDown, true
		default:
			b.show(ctx, chatID, msgID, view.ProviderPay(*o, b.Orders.Providers.Title(provider)))
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
			b.show(ctx, chatID, msgID, view.Plain("Заказ отменён."))
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
			answer.Text = view.TxtOrderDone
		case err != nil:
			b.Log.Error("order action", "data", q.Data, "err", err)
			answer.Text, answer.ShowAlert = view.TxtDown, true
		}
		return
	}
}

// InvoiceLink is payments.Stars' LinkFunc: a Telegram Stars invoice link.
func (b *Bot) InvoiceLink(ctx context.Context, title, description, payload string, stars int) (string, error) {
	return b.tg.CreateInvoiceLink(ctx, &tgbot.CreateInvoiceLinkParams{
		Title: title, Description: description, Payload: payload, Currency: "XTR",
		Prices: []models.LabeledPrice{{Label: "Оплата", Amount: stars}},
	})
}

// onPreCheckout answers Telegram's "may this payment go ahead?" within its
// 10 s limit: only for an order of this very user that still waits for it.
func (b *Bot) onPreCheckout(ctx context.Context, q *models.PreCheckoutQuery) {
	ans := &tgbot.AnswerPreCheckoutQueryParams{PreCheckoutQueryID: q.ID, OK: true}
	if b.Orders == nil || q.Currency != "XTR" {
		ans.OK, ans.ErrorMessage = false, "Оплата недоступна."
	} else if err := b.Orders.CheckInvoice(ctx, "stars", q.InvoicePayload, q.From.ID, float64(q.TotalAmount), q.Currency); err != nil {
		ans.OK, ans.ErrorMessage = false, "Этот счёт больше недействителен. Создайте заказ заново."
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
	for _, p := range b.Orders.Providers.Enabled() {
		out = append(out, view.ProviderButton{ID: p.ID(), Title: p.Title()})
	}
	return out
}
