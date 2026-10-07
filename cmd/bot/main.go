// Command bot runs the Telegram bot and its WebApp server in one process.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vpn-control-bot/internal/access"
	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/notify"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/telegram"
	"vpn-control-bot/internal/vpnctl"
	"vpn-control-bot/internal/webapp"
	"vpn-control-bot/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	api := vpnctl.New(cfg.VpnctlURL, cfg.VpnctlToken)
	codec := refcode.New(cfg.RefSecret)
	acc := access.New(api, codec, cfg.Mode, st)

	svc := &orders.Service{
		DB: st.DB(), Chats: st, API: api, PayMode: cfg.PayMode, Manual: cfg.ManualEnabled(),
		Details: cfg.ManualPayDetails, TTL: cfg.OrderTTL, Now: time.Now, Log: log,
	}
	bot, err := telegram.New(ctx, cfg.BotToken, telegram.Deps{
		Access: acc, API: api, Codec: codec, Orders: svc, Prompts: st, AdminChatID: cfg.AdminChatID, IsAdmin: cfg.IsAdmin,
		WebAppURL: cfg.WebAppURL(), PayMode: cfg.PayMode, Log: log,
	})
	if err != nil {
		return err
	}
	svc.Notify = bot
	if cfg.ManualEnabled() {
		bot.CheckAdminChat(ctx)
	}
	app := webapp.New(webapp.Deps{
		API: api, Codec: codec, BotToken: cfg.BotToken, BotUsername: bot.Username(),
		Path: cfg.WebAppPath, PayMode: cfg.PayMode, Orders: svc, Static: web.Dist, Log: log,
	})

	errc := make(chan error, 1)
	go func() { errc <- app.Listen(cfg.HTTPAddr) }()
	go bot.Run(ctx)
	if cfg.NotifyInterval > 0 {
		n := &notify.Runner{
			API: api, Store: st, Send: bot.SendNotice, Interval: cfg.NotifyInterval, Log: log,
			Sweep: svc.ExpireStale,
			Rules: notify.Rules{TrafficPct: cfg.TrafficAlertPct, RemindDays: cfg.RemindDays},
		}
		go n.Run(ctx)
	} else {
		go sweepLoop(ctx, svc, log)
	}
	log.Info("started", "mode", cfg.Mode, "pay", cfg.PayMode, "manual", cfg.ManualEnabled(), "notify", cfg.NotifyInterval, "bot", "@"+bot.Username(), "webapp", cfg.WebAppURL(), "addr", cfg.HTTPAddr)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	log.Info("shutting down")
	return app.Shutdown()
}

// sweepLoop expires stale orders when the notifier (which normally does it) is off.
func sweepLoop(ctx context.Context, svc *orders.Service, log *slog.Logger) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		if err := svc.ExpireStale(ctx); err != nil {
			log.Error("orders: sweep", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
