package telegram

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/telegram/view"
)

var (
	_ orders.Notifier = (*Bot)(nil)
	_ orders.Alerter  = (*Bot)(nil)
)

// NoticeStore remembers the message that carries an order's card in each admin's chat (*store.Store).
type NoticeStore interface {
	SaveNotice(ctx context.Context, orderID string, adminID int64, msgID int) error
	Notices(ctx context.Context, orderID string) (map[int64]int, error)
}

func (b *Bot) isAdmin(id int64) bool { return b.IsAdmin != nil && b.IsAdmin(id) }

// needsDecision: the states in which admins must act, so a card is sent even to those who have none yet.
func needsDecision(s orders.Status) bool {
	return s == orders.StatusPendingReview || s == orders.StatusFailed
}

// Card sends every admin a card for an order that needs a decision and edits the cards already sent when it
// moves on. Admins are tried independently: one who never pressed /start (Telegram answers 403) does not hold
// back the others; the joined error names who failed.
func (b *Bot) Card(ctx context.Context, o orders.Order) error {
	have, err := b.Notices.Notices(ctx, o.ID)
	if err != nil {
		return err
	}
	if len(have) == 0 && !needsDecision(o.Status) {
		return nil
	}
	cust := b.adminCustomer(ctx, o)
	page := b.links.Page("/admin/orders/" + o.ID)
	var errs []error
	for _, id := range b.Admins {
		var err error
		s := view.AdminCard(b.locFor(ctx, id), o, cust, page)
		if msgID, ok := have[id]; ok {
			err = b.editCard(ctx, id, msgID, s)
			if isGoneMessage(err) {
				// The card was deleted or can no longer be edited: send a fresh one if somebody has to decide,
				// otherwise there is nothing to keep in sync.
				if needsDecision(o.Status) {
					err = b.sendCard(ctx, id, o.ID, s)
				} else {
					err = nil
				}
			}
		} else if needsDecision(o.Status) {
			err = b.sendCard(ctx, id, o.ID, s)
		} else {
			continue // this admin had no card and the order needs nobody now
		}
		b.track(id, err)
		if err != nil {
			errs = append(errs, fmt.Errorf("admin %d: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (b *Bot) sendCard(ctx context.Context, adminID int64, orderID string, s view.Screen) error {
	m, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: adminID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	if err != nil {
		return err
	}
	return b.Notices.SaveNotice(ctx, orderID, adminID, m.ID)
}

func (b *Bot) editCard(ctx context.Context, adminID int64, msgID int, s view.Screen) error {
	_, err := b.tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: adminID, MessageID: msgID, Text: s.Text,
		ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard})
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

// isGoneMessage: Telegram answered that the message to edit is missing or not editable.
func isGoneMessage(err error) bool {
	if err == nil || !errors.Is(err, tgbot.ErrorBadRequest) {
		return false
	}
	msg := err.Error()
	for _, m := range []string{"message to edit not found", "MESSAGE_ID_INVALID", "message can't be edited"} {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// track remembers which admins the bot could not reach, for the Orders screen.
func (b *Bot) track(adminID int64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		if !errors.Is(err, tgbot.ErrorForbidden) {
			b.Log.Warn("admin notification failed", "admin", adminID, "err", err)
			return // not a sign the admin is unreachable: leave the flag as it was
		}
		if b.unreachable == nil {
			b.unreachable = map[int64]bool{}
		}
		b.unreachable[adminID] = true
		b.Log.Warn("admin notification failed: the admin must press /start in the bot", "admin", adminID, "err", err)
		return
	}
	delete(b.unreachable, adminID)
}

// Unreachable lists the admins the last send to failed for.
func (b *Bot) Unreachable() []int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]int64, 0, len(b.unreachable))
	for id := range b.unreachable {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Alert sends a plain message to every admin in their language (no parse mode: the arguments may hold any characters).
func (b *Bot) Alert(ctx context.Context, a orders.Alert) error {
	var errs []error
	for _, id := range b.Admins {
		_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: id, Text: b.locFor(ctx, id).Render(a)})
		b.track(id, err)
		if err != nil {
			errs = append(errs, fmt.Errorf("admin %d: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// NotifyAdmins sends every admin m in their language with a button into the given admin page ("/admin/methods").
func (b *Bot) NotifyAdmins(ctx context.Context, m i18n.Msg, page string) {
	for _, id := range b.Admins {
		loc := b.locFor(ctx, id)
		s := view.AdminEntry(loc, loc.Render(m), b.links.Page(page))
		_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: id, Text: s.Text, ReplyMarkup: s.Keyboard})
		b.track(id, err)
	}
}

// onAdminCmd: /admin opens the admin part for an admin.
func (b *Bot) onAdminCmd(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	m := u.Message
	if m.Chat.Type != models.ChatTypePrivate || !b.isAdmin(m.From.ID) {
		return
	}
	s := view.AdminEntry(loc, loc.T("msg.admin_open"), b.links.Page("/admin"))
	if _, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: s.Text, ReplyMarkup: s.Keyboard}); err != nil {
		b.Log.Error("admin command", "err", err)
	}
}
