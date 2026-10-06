package telegram

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// privateOnly lets through only messages and callbacks from a known sender
// in a private chat: in a group the bot would post one member's data for
// everyone, and handlers rely on Message.From / CallbackQuery.Message.
func privateOnly(u *models.Update) bool {
	switch {
	case u.Message != nil:
		return u.Message.From != nil && u.Message.Chat.Type == models.ChatTypePrivate
	case u.CallbackQuery != nil:
		m := u.CallbackQuery.Message.Message
		return m != nil && m.Chat.Type == models.ChatTypePrivate
	}
	return false
}

func privateMiddleware(next tgbot.HandlerFunc) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
		if privateOnly(u) {
			next(ctx, b, u)
		}
	}
}
