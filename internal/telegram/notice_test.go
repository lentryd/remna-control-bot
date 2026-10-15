package telegram

import (
	"context"
	"errors"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/notify"
)

func TestSendNoticeUnreachable(t *testing.T) {
	b, _, _ := newCardBot(t)
	for chat, want := range map[int64]bool{
		2: true,  // never pressed /start
		4: true,  // chat not found
		1: false, // delivered
	} {
		err := b.SendNotice(context.Background(), notify.Notice{Kind: notify.Traffic, ChatID: chat, Limit: 1})
		if got := errors.Is(err, notify.ErrUnreachable); got != want {
			t.Errorf("chat %d: err %v, unreachable %v, want %v", chat, err, got, want)
		}
	}
}

type memReach map[int64]bool

func (m memReach) SetUnreachable(_ context.Context, id int64, on bool) error { m[id] = on; return nil }

func TestMiddlewareMarksSenderReachable(t *testing.T) {
	reach := memReach{1: true}
	b := &Bot{Deps: Deps{Reach: reach}}
	called := false
	h := b.reachMiddleware()(func(context.Context, *tgbot.Bot, *models.Update) { called = true })
	h(context.Background(), nil, &models.Update{Message: &models.Message{From: &models.User{ID: 1}}})
	if reach[1] || !called {
		t.Fatalf("unreachable %v, handler called %v", reach[1], called)
	}
}
