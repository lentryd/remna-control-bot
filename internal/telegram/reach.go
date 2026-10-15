package telegram

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ReachStore pauses notifications to chats the bot can't write to (*store.Store).
type ReachStore interface {
	SetUnreachable(ctx context.Context, chatID int64, on bool) error
}

// reachMiddleware resumes notifications to whoever writes to the bot: a private chat's id is the sender's id.
func (b *Bot) reachMiddleware() tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, tg *tgbot.Bot, u *models.Update) {
			if id, _, ok := langFrom(u); ok && b.Reach != nil {
				if err := b.Reach.SetUnreachable(ctx, id, false); err != nil && b.Log != nil {
					b.Log.Warn("store reachability", "err", err)
				}
			}
			next(ctx, tg, u)
		}
	}
}
