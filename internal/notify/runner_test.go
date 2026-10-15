package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"remna-control-bot/internal/remnactl"
)

type memState struct {
	sent map[string]bool
	off  map[int64]bool
}

func newState() *memState { return &memState{sent: map[string]bool{}, off: map[int64]bool{}} }

func (m *memState) NoticeSent(_ context.Context, k string) (bool, error) { return m.sent[k], nil }
func (m *memState) MarkNoticeSent(_ context.Context, k string) error     { m.sent[k] = true; return nil }
func (m *memState) Unreachable(_ context.Context, chat int64) (bool, error) {
	return m.off[chat], nil
}
func (m *memState) SetUnreachable(_ context.Context, chat int64, on bool) error {
	m.off[chat] = on
	return nil
}

func TestRunOnceSendsEachNoticeOnce(t *testing.T) {
	f := remnactl.NewFake()
	f.Tariffs = tariffs
	f.Add(remnactl.Customer{ID: 1, TelegramID: ptr(int64(100)), Subscriptions: []remnactl.Subscription{
		sub(1, 1, &remnactl.RwUser{UsedTrafficBytes: 45 * GB, TrafficLimitBytes: 50 * GB, ExpireAt: until(48 * 3600e9)}),
	}})
	var sent []Notice
	fail := true
	r := &Runner{API: f, Store: newState(), Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Send: func(_ context.Context, n Notice) error {
			if fail && n.Kind == Reminder {
				return errors.New("telegram down")
			}
			sent = append(sent, n)
			return nil
		}}

	r.RunOnce(context.Background(), now)
	if len(sent) != 1 || sent[0].Kind != Traffic {
		t.Fatalf("first run: %+v", sent)
	}
	fail = false
	r.RunOnce(context.Background(), now) // traffic already sent; the failed reminder is retried
	if len(sent) != 2 || sent[1].Kind != Reminder {
		t.Fatalf("second run: %+v", sent)
	}
	r.RunOnce(context.Background(), now)
	if len(sent) != 2 {
		t.Fatalf("third run sent again: %+v", sent)
	}
}

func TestRunOnceSkipsUnreachableChat(t *testing.T) {
	f := remnactl.NewFake()
	f.Tariffs = tariffs
	f.Add(remnactl.Customer{ID: 1, TelegramID: ptr(int64(100)), Subscriptions: []remnactl.Subscription{
		sub(1, 1, &remnactl.RwUser{UsedTrafficBytes: 45 * GB, TrafficLimitBytes: 50 * GB, ExpireAt: until(48 * 3600e9)}),
	}}) // two notices: traffic and a reminder
	st := newState()
	attempts, delivered := 0, 0
	started := false
	r := &Runner{API: f, Store: st, Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Send: func(context.Context, Notice) error {
			attempts++
			if !started {
				return fmt.Errorf("%w: forbidden", ErrUnreachable)
			}
			delivered++
			return nil
		}}

	r.RunOnce(context.Background(), now)
	if attempts != 1 || !st.off[100] {
		t.Fatalf("first run: %d attempts, unreachable %v; want 1 attempt and the chat marked", attempts, st.off[100])
	}
	r.RunOnce(context.Background(), now)
	if attempts != 1 {
		t.Fatalf("second run tried an unreachable chat: %d attempts", attempts)
	}
	started = true
	st.off[100] = false // the customer wrote to the bot
	r.RunOnce(context.Background(), now)
	if delivered != 2 {
		t.Fatalf("after /start: %d delivered, want both pending notices", delivered)
	}
}

func TestRunOnceOtherErrorKeepsChatReachable(t *testing.T) {
	f := remnactl.NewFake()
	f.Tariffs = tariffs
	f.Add(remnactl.Customer{ID: 1, TelegramID: ptr(int64(100)), Subscriptions: []remnactl.Subscription{
		sub(1, 1, &remnactl.RwUser{UsedTrafficBytes: 45 * GB, TrafficLimitBytes: 50 * GB, ExpireAt: until(48 * 3600e9)}),
	}})
	st := newState()
	attempts := 0
	r := &Runner{API: f, Store: st, Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Send: func(context.Context, Notice) error { attempts++; return errors.New("telegram down") }}
	r.RunOnce(context.Background(), now)
	if attempts != 2 || st.off[100] {
		t.Fatalf("%d attempts, unreachable %v; want both tried and the chat not marked", attempts, st.off[100])
	}
}

func TestRunOncePanelDown(t *testing.T) {
	f := remnactl.NewFake()
	f.Err = remnactl.ErrUnavailable
	called := false
	r := &Runner{API: f, Store: newState(), Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Send: func(context.Context, Notice) error { called = true; return nil }}
	r.RunOnce(context.Background(), now)
	if called {
		t.Fatal("sent while the panel is down")
	}
}

func TestRunOnceCallsSweep(t *testing.T) {
	r := &Runner{API: remnactl.NewFake(), Store: newState(), Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Send: func(context.Context, Notice) error { return nil }}
	called := false
	r.Sweep = func(context.Context) error { called = true; return nil }
	r.RunOnce(context.Background(), now)
	if !called {
		t.Fatal("sweep not called")
	}
}

func TestDue(t *testing.T) {
	now := time.Now()
	for name, c := range map[string]struct {
		last     time.Time
		interval time.Duration
		want     bool
	}{
		"off":            {time.Time{}, 0, false},
		"first pass":     {time.Time{}, time.Hour, true},
		"too early":      {now.Add(-30 * time.Minute), time.Hour, false},
		"interval gone":  {now.Add(-time.Hour), time.Hour, true},
		"turned off now": {now.Add(-time.Hour), 0, false},
	} {
		if got := due(c.last, now, c.interval); got != c.want {
			t.Errorf("%s: due = %v, want %v", name, got, c.want)
		}
	}
}
