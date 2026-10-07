package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestParseAmount(t *testing.T) {
	for in, want := range map[string]float64{"700": 700, "699,50": 699.5, " 1 200 ₽ ": 1200, "500.5р": 500.5} {
		if got, ok := parseAmount(in); !ok || got != want {
			t.Errorf("%q = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "abc", "0", "-5", "1e9"} {
		if _, ok := parseAmount(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestAdminGate(t *testing.T) {
	const admin = int64(-100)
	group := models.Chat{ID: admin, Type: models.ChatTypeSupergroup}
	cases := []struct {
		name string
		u    *models.Update
		want bool
	}{
		{"private message", &models.Update{Message: &models.Message{From: &models.User{ID: 1}, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}}}, true},
		{"admin button", &models.Update{CallbackQuery: &models.CallbackQuery{Data: "oa:0c6f2b3a-5d4e-4f1a-9b7c-8e2d1a3f4b5c", Message: models.MaybeInaccessibleMessage{Message: &models.Message{Chat: group}}}}, true},
		{"customer button in admin chat", &models.Update{CallbackQuery: &models.CallbackQuery{Data: "m:home", Message: models.MaybeInaccessibleMessage{Message: &models.Message{Chat: group}}}}, false},
		{"reply in admin chat", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: group, ReplyToMessage: &models.Message{ID: 3}}}, true},
		{"/start in admin chat", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: group, Text: "/start"}}, false},
		{"reply /menu in admin chat", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: group, Text: "/menu", ReplyToMessage: &models.Message{ID: 3},
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: 5}}}}, false},
		{"reply /start@bot in admin chat", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: group, Text: "/start@bot", ReplyToMessage: &models.Message{ID: 3}}}, false},
		{"other group", &models.Update{Message: &models.Message{From: &models.User{ID: 5}, Chat: models.Chat{ID: -7, Type: models.ChatTypeGroup}, ReplyToMessage: &models.Message{}}}, false},
	}
	for _, c := range cases {
		if got := allowed(c.u, admin); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
