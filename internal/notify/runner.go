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

// Conf is what the runner reads before every pass, so admins can change it without a restart.
type Conf struct {
	Interval time.Duration // 0: notifications are off
	Rules    Rules
}

// tick is how often Run looks at Conf; Interval decides whether a pass is due.
const tick = time.Minute

type Runner struct {
	API   vpnctl.Client
	Store Sent
	Send  func(ctx context.Context, n Notice) error
	Conf  func() Conf
	Log   *slog.Logger
	Sweep func(ctx context.Context) error // housekeeping on every pass (expired orders); optional
}

// due: notifications are on and Interval has passed since the last pass.
func due(last, now time.Time, interval time.Duration) bool {
	return interval > 0 && (last.IsZero() || now.Sub(last) >= interval)
}

// Run checks every minute whether a pass is due, until ctx is done.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	var last time.Time
	for {
		if now := time.Now(); due(last, now, r.Conf().Interval) {
			last = now
			r.RunOnce(ctx, now)
		}
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
	if r.Sweep != nil {
		if err := r.Sweep(ctx); err != nil {
			r.Log.Error("notify: sweep", "err", err)
		}
	}
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
	for _, n := range Plan(customers, subs, tariffs, r.Conf().Rules, now) {
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
