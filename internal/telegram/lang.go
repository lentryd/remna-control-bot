package telegram

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/i18n"
)

// LangStore remembers each person's Telegram language for messages the bot sends on its own (*store.Store).
type LangStore interface {
	SetLang(ctx context.Context, tgID int64, lang string) error
	Lang(ctx context.Context, tgID int64) (string, error)
}

// langFrom returns the sender of an update and the language_code Telegram reports for them.
func langFrom(u *models.Update) (tgID int64, code string, ok bool) {
	switch {
	case u.Message != nil && u.Message.From != nil:
		return u.Message.From.ID, u.Message.From.LanguageCode, true
	case u.CallbackQuery != nil:
		return u.CallbackQuery.From.ID, u.CallbackQuery.From.LanguageCode, true
	case u.PreCheckoutQuery != nil && u.PreCheckoutQuery.From != nil:
		return u.PreCheckoutQuery.From.ID, u.PreCheckoutQuery.From.LanguageCode, true
	}
	return 0, "", false
}

// langMiddleware stores the sender's language and puts their Localizer into the context.
func (b *Bot) langMiddleware() tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, tg *tgbot.Bot, u *models.Update) {
			if id, code, ok := langFrom(u); ok {
				loc := i18n.Default.For(code)
				if code == "" {
					loc = b.locFor(ctx, id)
				} else if b.Langs != nil {
					if err := b.Langs.SetLang(ctx, id, loc.Lang()); err != nil && b.Log != nil {
						b.Log.Warn("store language", "err", err)
					}
				}
				ctx = i18n.NewContext(ctx, loc)
			}
			next(ctx, tg, u)
		}
	}
}

// loc is the localizer of the person whose update is being handled (English outside a handler).
func (b *Bot) loc(ctx context.Context) i18n.Localizer {
	return i18n.FromContext(ctx)
}

// locFor is the localizer for a person the bot writes to unprompted; English when their language is unknown.
func (b *Bot) locFor(ctx context.Context, tgID int64) i18n.Localizer {
	if b.Langs == nil {
		return i18n.Default.For("")
	}
	code, err := b.Langs.Lang(ctx, tgID)
	if err != nil && b.Log != nil {
		b.Log.Warn("load language", "err", err)
	}
	return i18n.Default.For(code)
}
