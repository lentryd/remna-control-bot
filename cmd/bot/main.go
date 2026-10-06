// Command bot runs the Telegram bot and its WebApp server in one process.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vpn-control-bot/internal/access"
	"vpn-control-bot/internal/billing"
	"vpn-control-bot/internal/config"
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

	bot, err := telegram.New(ctx, cfg.BotToken, telegram.Deps{
		Access: acc, API: api, Codec: codec, Billing: billing.NotImplemented{}, WebAppURL: cfg.WebAppURL(), Log: log,
	})
	if err != nil {
		return err
	}
	app := webapp.New(webapp.Deps{
		API: api, Codec: codec, BotToken: cfg.BotToken, BotUsername: bot.Username(),
		Path: cfg.WebAppPath, Static: web.Dist, Log: log,
	})

	errc := make(chan error, 1)
	go func() { errc <- app.Listen(cfg.HTTPAddr) }()
	go bot.Run(ctx)
	log.Info("started", "mode", cfg.Mode, "bot", "@"+bot.Username(), "webapp", cfg.WebAppURL(), "addr", cfg.HTTPAddr)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	log.Info("shutting down")
	return app.Shutdown()
}
