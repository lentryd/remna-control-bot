package telegram

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// allowed lets through private chats with a known sender: in a group the customer screens would post one member's
// data for everyone, and admins no longer work in chats.
func allowed(u *models.Update) bool {
	switch {
	case u.Message != nil:
		return u.Message.From != nil && u.Message.Chat.Type == models.ChatTypePrivate
	case u.CallbackQuery != nil:
		m := u.CallbackQuery.Message.Message
		return m != nil && m.Chat.Type == models.ChatTypePrivate
	case u.PreCheckoutQuery != nil:
		return u.PreCheckoutQuery.From != nil
	}
	return false
}

func gate() tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
			if allowed(u) {
				next(ctx, b, u)
			}
		}
	}
}
