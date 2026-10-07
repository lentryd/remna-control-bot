package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestPrivateOnly(t *testing.T) {
	from := &models.User{ID: 7}
	priv := models.Chat{ID: 7, Type: models.ChatTypePrivate}
	group := models.Chat{ID: -100, Type: models.ChatTypeGroup}
	cbMsg := func(c models.Chat) models.MaybeInaccessibleMessage {
		return models.MaybeInaccessibleMessage{Message: &models.Message{ID: 5, Chat: c}}
	}
	cases := []struct {
		name string
		u    *models.Update
		ok   bool
	}{
		{"private message", &models.Update{Message: &models.Message{From: from, Chat: priv}}, true},
		{"group message", &models.Update{Message: &models.Message{From: from, Chat: group}}, false},
		{"message without sender", &models.Update{Message: &models.Message{Chat: priv}}, false},
		{"private callback", &models.Update{CallbackQuery: &models.CallbackQuery{From: *from, Message: cbMsg(priv)}}, true},
		{"group callback", &models.Update{CallbackQuery: &models.CallbackQuery{From: *from, Message: cbMsg(group)}}, false},
		{"stale callback", &models.Update{CallbackQuery: &models.CallbackQuery{From: *from}}, false},
		{"nothing", &models.Update{}, false},
	}
	for _, c := range cases {
		if got := allowed(c.u, 0); got != c.ok {
			t.Errorf("%s: allowed = %v, want %v", c.name, got, c.ok)
		}
	}
}

func TestAllowedPreCheckout(t *testing.T) {
	u := &models.Update{PreCheckoutQuery: &models.PreCheckoutQuery{ID: "1", From: &models.User{ID: 5}}}
	if !allowed(u, -100) {
		t.Fatal("pre_checkout_query must reach the bot: Telegram wants an answer within 10 s")
	}
	u.PreCheckoutQuery.From = nil
	if allowed(u, -100) {
		t.Fatal("a query without a sender is not ours")
	}
}
