package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

type memSent map[string]bool

func (m memSent) NoticeSent(_ context.Context, k string) (bool, error) { return m[k], nil }
func (m memSent) MarkNoticeSent(_ context.Context, k string) error     { m[k] = true; return nil }

func TestRunOnceSendsEachNoticeOnce(t *testing.T) {
	f := vpnctl.NewFake()
	f.Tariffs = tariffs
	f.Add(vpnctl.Customer{ID: 1, TelegramID: ptr(int64(100)), Subscriptions: []vpnctl.Subscription{
		sub(1, 1, &vpnctl.RwUser{UsedTrafficBytes: 45 * GB, TrafficLimitBytes: 50 * GB, ExpireAt: until(48 * 3600e9)}),
	}})
	var sent []Notice
	fail := true
	r := &Runner{API: f, Store: memSent{}, Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
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

func TestRunOncePanelDown(t *testing.T) {
	f := vpnctl.NewFake()
	f.Err = vpnctl.ErrUnavailable
	called := false
	r := &Runner{API: f, Store: memSent{}, Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Send: func(context.Context, Notice) error { called = true; return nil }}
	r.RunOnce(context.Background(), now)
	if called {
		t.Fatal("sent while the panel is down")
	}
}

func TestRunOnceCallsSweep(t *testing.T) {
	r := &Runner{API: vpnctl.NewFake(), Store: memSent{}, Conf: func() Conf { return Conf{Rules: rules} }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
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
