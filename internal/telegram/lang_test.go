package telegram

import (
	"context"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/i18n"
)

type fakeLangs map[int64]string

func (f fakeLangs) SetLang(_ context.Context, id int64, l string) error {
	if l != "" {
		f[id] = l
	}
	return nil
}
func (f fakeLangs) Lang(_ context.Context, id int64) (string, error) { return f[id], nil }

func TestLangFrom(t *testing.T) {
	msg := &models.Update{Message: &models.Message{From: &models.User{ID: 5, LanguageCode: "ru"}}}
	if id, code, ok := langFrom(msg); !ok || id != 5 || code != "ru" {
		t.Fatalf("message: %v %q %v", id, code, ok)
	}
	cb := &models.Update{CallbackQuery: &models.CallbackQuery{From: models.User{ID: 6, LanguageCode: "de"}}}
	if id, code, ok := langFrom(cb); !ok || id != 6 || code != "de" {
		t.Fatalf("callback: %v %q %v", id, code, ok)
	}
	if _, _, ok := langFrom(&models.Update{}); ok {
		t.Fatal("empty update must report !ok")
	}
}

func TestLocForUnknownIsEnglish(t *testing.T) {
	b := &Bot{Deps: Deps{Langs: fakeLangs{}}}
	if got := b.locFor(context.Background(), 99).Lang(); got != "en" {
		t.Fatalf("got %q", got)
	}
	b = &Bot{} // no store at all
	if got := b.locFor(context.Background(), 99).Lang(); got != "en" {
		t.Fatalf("nil store: got %q", got)
	}
}

func TestLocForUsesStoredLanguage(t *testing.T) {
	b := &Bot{Deps: Deps{Langs: fakeLangs{3: "ru"}}}
	if got := b.locFor(context.Background(), 3).Lang(); got != "ru" {
		t.Fatalf("got %q", got)
	}
}

func TestMiddlewareStoresAndInjects(t *testing.T) {
	langs := fakeLangs{}
	b := &Bot{Deps: Deps{Langs: langs}}
	var seen i18n.Localizer
	h := b.langMiddleware()(func(ctx context.Context, _ *tgbot.Bot, u *models.Update) { seen = b.loc(ctx) })
	h(context.Background(), nil, &models.Update{Message: &models.Message{From: &models.User{ID: 1, LanguageCode: "ru-RU"}}})
	if seen.Lang() != "ru" || langs[1] != "ru" {
		t.Fatalf("seen %q, stored %q", seen.Lang(), langs[1])
	}
	// the user switches Telegram to English
	h(context.Background(), nil, &models.Update{Message: &models.Message{From: &models.User{ID: 1, LanguageCode: "en"}}})
	if seen.Lang() != "en" || langs[1] != "en" {
		t.Fatalf("seen %q, stored %q", seen.Lang(), langs[1])
	}
	// no language_code: keep the stored one, answer in it
	h(context.Background(), nil, &models.Update{Message: &models.Message{From: &models.User{ID: 1}}})
	if seen.Lang() != "en" {
		t.Fatalf("empty code: seen %q", seen.Lang())
	}
}
