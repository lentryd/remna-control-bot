package telegram

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/telegram/view"
	"vpn-control-bot/internal/vpnctl"
)

// amountCleaner strips spaces, the ruble spellings and the current currency's
// symbol and ISO code (the input is lower-cased first), decimal comma → dot.
func amountCleaner() *strings.Replacer {
	pairs := []string{" ", "", "\u00a0", "", "₽", "", "руб", "", "р", ""}
	for _, h := range view.CurrencyHints() {
		pairs = append(pairs, strings.ToLower(h), "")
	}
	return strings.NewReplacer(append(pairs, ",", ".")...)
}

func parseAmount(s string) (float64, bool) {
	v, err := strconv.ParseFloat(amountCleaner().Replace(strings.ToLower(strings.TrimSpace(s))), 64)
	if err != nil || math.IsNaN(v) || v <= 0 || v > 1_000_000 {
		return 0, false
	}
	return math.Round(v*100) / 100, true
}

func adminName(u *models.User) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

// CheckAdminChat warns at startup when the bot can't see the admin chat: the
// first receipt card would fail otherwise.
func (b *Bot) CheckAdminChat(ctx context.Context) {
	if _, err := b.tg.GetChat(ctx, &tgbot.GetChatParams{ChatID: b.AdminChatID}); err != nil {
		b.Log.Warn("admin chat is unreachable: add the bot to ADMIN_CHAT_ID", "chat", b.AdminChatID, "err", err)
	}
}

func (b *Bot) isAdmin(id int64) bool { return b.IsAdmin != nil && b.IsAdmin(id) }

func (b *Bot) onAdmin(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { b.tg.AnswerCallbackQuery(ctx, answer) }()
	if b.Orders == nil || !b.isAdmin(q.From.ID) {
		answer.Text, answer.ShowAlert = view.TxtNoRights, true
		return
	}
	chatID := q.Message.Message.Chat.ID
	who := adminName(&q.From)
	for prefix, d := range map[string]orders.Decision{view.CbAdminApprove: orders.DecideFull, view.CbAdminDebt: orders.DecideDebt, view.CbAdminCredit: orders.DecideCredit} {
		if id, ok := parseOrderID(q.Data, prefix); ok {
			o, err := b.Orders.Approve(ctx, id, who, d)
			b.adminResult(answer, o, err)
			return
		}
	}
	if id, ok := parseOrderID(q.Data, view.CbAdminRetry); ok {
		o, err := b.Orders.Retry(ctx, id, who)
		b.adminResult(answer, o, err)
		return
	}
	if id, ok := parseOrderID(q.Data, view.CbAdminNoReason); ok {
		o, err := b.Orders.Reject(ctx, id, "", who)
		b.adminResult(answer, o, err)
		return
	}
	if id, ok := parseOrderID(q.Data, view.CbAdminEdit); ok {
		b.prompt(ctx, chatID, q.Message.Message.ID, fmt.Sprintf(view.TxtAskAmount, orders.ShortID(id)), store.Prompt{Kind: "amount", OrderID: id}, nil)
		return
	}
	if id, ok := parseOrderID(q.Data, view.CbAdminReject); ok {
		noReason := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: view.BtnNoReason, CallbackData: view.CbAdminNoReason + id}}}}
		b.prompt(ctx, chatID, q.Message.Message.ID, fmt.Sprintf(view.TxtAskReason, orders.ShortID(id)), store.Prompt{Kind: "reject", OrderID: id}, noReason)
	}
}

// adminResult answers the pressed button. A panel failure is not an error:
// the order comes back `failed` (its card already shows Retry), so say why.
func (b *Bot) adminResult(answer *tgbot.AnswerCallbackQueryParams, o *orders.Order, err error) {
	switch {
	case err == nil:
		if o != nil && o.Status == orders.StatusFailed {
			answer.Text, answer.ShowAlert = alert("⚠️ "+o.Error), true
		}
	case errors.Is(err, orders.ErrStateChanged), errors.Is(err, orders.ErrNotFound):
		answer.Text = view.TxtOrderDone
	case errors.Is(err, vpnctl.ErrUnavailable):
		answer.Text, answer.ShowAlert = view.TxtPanelDown, true
	default:
		b.Log.Error("admin action", "err", err)
		answer.Text, answer.ShowAlert = alert(err.Error()), true
	}
}

// prompt asks the admins a question answered by replying to it. A forced
// reply only works for a reply markup, so the "no reason" button goes in a
// second edit when given.
func (b *Bot) prompt(ctx context.Context, chatID int64, cardID int, text string, p store.Prompt, extra *models.InlineKeyboardMarkup) {
	m, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: text,
		ReplyParameters: &models.ReplyParameters{MessageID: cardID},
		ReplyMarkup:     &models.ForceReply{ForceReply: true, Selective: true}})
	if err != nil {
		b.Log.Error("admin prompt", "err", err)
		return
	}
	if err := b.Prompts.SavePrompt(ctx, chatID, m.ID, p); err != nil {
		b.Log.Error("admin prompt", "err", err)
	}
	if extra != nil {
		b.tg.EditMessageReplyMarkup(ctx, &tgbot.EditMessageReplyMarkupParams{ChatID: chatID, MessageID: m.ID, ReplyMarkup: extra})
	}
}

// onAdminReply handles a reply to a prompt (amount or reject reason); it
// reports whether the message answered one.
func (b *Bot) onAdminReply(ctx context.Context, m *models.Message) bool {
	if b.Orders == nil || b.Prompts == nil || m.ReplyToMessage == nil || m.From == nil || !b.isAdmin(m.From.ID) {
		return false
	}
	p, ok, err := b.Prompts.TakePrompt(ctx, m.Chat.ID, m.ReplyToMessage.ID)
	if err != nil || !ok {
		return false
	}
	who := adminName(m.From)
	reply := func(text string) {
		b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: text, ReplyParameters: &models.ReplyParameters{MessageID: m.ID}})
	}
	switch p.Kind {
	case "amount":
		v, ok := parseAmount(m.Text)
		if !ok {
			b.Prompts.SavePrompt(ctx, m.Chat.ID, m.ReplyToMessage.ID, p) // keep asking
			reply(view.TxtBadAmount)
			return true
		}
		if _, err := b.Orders.SetReceived(ctx, p.OrderID, v, who); err != nil {
			reply(errText(err))
		}
	case "reject":
		if _, err := b.Orders.Reject(ctx, p.OrderID, m.Text, who); err != nil {
			reply(errText(err))
		}
	}
	return true
}

func errText(err error) string {
	if errors.Is(err, orders.ErrStateChanged) {
		return view.TxtOrderDone
	}
	return "⚠️ " + err.Error()
}

// alert fits a callback answer (Telegram allows 200 characters).
func alert(s string) string {
	if r := []rune(s); len(r) > 190 {
		return string(r[:189]) + "…"
	}
	return s
}
