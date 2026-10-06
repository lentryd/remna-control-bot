package notify

import (
	"context"
	"log/slog"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

// Sent remembers which notices went out (implemented by store.Store).
type Sent interface {
	NoticeSent(ctx context.Context, key string) (bool, error)
	MarkNoticeSent(ctx context.Context, key string) error
}

// Runner checks the panel every Interval and sends new notices.
type Runner struct {
	API      vpnctl.Client
	Store    Sent
	Send     func(ctx context.Context, n Notice) error
	Rules    Rules
	Interval time.Duration
	Log      *slog.Logger
}

// Run checks right away, then every Interval, until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		r.RunOnce(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunOnce sends every planned notice that wasn't sent before. A failed send
// isn't recorded, so it is retried on the next run.
func (r *Runner) RunOnce(ctx context.Context, now time.Time) {
	customers, err := r.API.ListCustomers(ctx)
	if err != nil {
		r.Log.Error("notify: customers", "err", err)
		return
	}
	subs, err := r.API.ListSubscriptions(ctx)
	if err != nil {
		r.Log.Error("notify: subscriptions", "err", err)
		return
	}
	tariffs, err := r.API.ListTariffs(ctx)
	if err != nil {
		r.Log.Error("notify: tariffs", "err", err)
		return
	}
	for _, n := range Plan(customers, subs, tariffs, r.Rules, now) {
		sent, err := r.Store.NoticeSent(ctx, n.Key)
		if err != nil {
			r.Log.Error("notify: state", "err", err)
			return
		}
		if sent {
			continue
		}
		if err := r.Send(ctx, n); err != nil {
			r.Log.Warn("notify: send", "key", n.Key, "chat", n.ChatID, "err", err)
			continue
		}
		if err := r.Store.MarkNoticeSent(ctx, n.Key); err != nil {
			r.Log.Error("notify: state", "err", err)
		}
		r.Log.Info("notify: sent", "key", n.Key, "chat", n.ChatID)
	}
}
