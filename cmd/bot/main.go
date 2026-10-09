// Command bot runs the Telegram bot and its WebApp server in one process.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vpn-control-bot/internal/access"
	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/currency"
	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/notify"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/payments"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/settings"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/telegram"
	"vpn-control-bot/internal/telegram/view"
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
	set, err := settings.Open(ctx, st.DB(), cfg.RefSecret)
	if err != nil {
		return err
	}
	for _, k := range set.Lost() {
		log.Warn("stored secrets cannot be decrypted (REF_SECRET changed?): re-enter them in the admin settings", "section", k)
	}

	api := vpnctl.New(cfg.VpnctlURL, cfg.VpnctlToken)
	cur := &currency.Source{API: api, Log: log, OnChange: view.SetCurrency}
	cur.SetFallback(fallbackCurrency(set.Snapshot()))
	base := cur.Get(ctx) // resolves once at startup: with the panel down the fallback is used and a warning is logged
	codec := refcode.New(cfg.RefSecret)
	mode := func() config.Mode { return set.Snapshot().General().Mode }
	payMode := func() config.PayMode { return set.Snapshot().General().PayMode }
	acc := access.New(api, codec, mode, st)

	svc := &orders.Service{DB: st.DB(), Chats: st, API: api, Now: time.Now, Log: log}
	svc.BaseCurrency = cur.Get
	// Provider invoices need a confirmed currency: the panel answered, or the operator set one explicitly.
	svc.CurrencyKnown = func() bool { return cur.Known() || set.Snapshot().General().BaseCurrency != "" }

	var bot *telegram.Bot
	starsLink := func(ctx context.Context, title, description, payload string, stars int) (string, error) {
		return bot.InvoiceLink(ctx, title, description, payload, stars)
	}
	apply := func(snap settings.Snapshot) {
		reg := payments.Build(payments.BuildInput{Settings: snap, PublicURL: cfg.PublicURL, WebAppURL: cfg.WebAppURL(), StarsLink: starsLink})
		g, m := snap.General(), snap.Manual()
		cur.SetFallback(fallbackCurrency(snap))
		svc.SetLive(orders.Live{PayMode: g.PayMode, Manual: manualOn(cfg, m), Details: m.Details, TTL: g.OrderTTL(), Providers: reg})
		log.Info("settings applied", "mode", g.Mode, "pay", g.PayMode, "manual", manualOn(cfg, m), "providers", reg.IDs())
		if len(reg.IDs()) > 0 && !strings.HasPrefix(cfg.PublicURL, "https://") {
			log.Warn("payment providers are on but PUBLIC_URL is not https: webhooks will not reach the bot", "url", cfg.PublicURL)
		}
	}
	apply(set.Snapshot())
	set.OnChange(apply)
	if len(svc.Live().Providers.IDs()) > 0 {
		log.Info("payment providers invoice in the base currency", "currency", base, "providers", svc.Live().Providers.IDs())
	}
	unconfirmed := len(svc.Live().Providers.IDs()) > 0 && !svc.CurrencyKnown()
	if unconfirmed {
		logCurrencyUnconfirmed(log, base)
	}

	bot, err = telegram.New(ctx, cfg.BotToken, telegram.Deps{
		Access: acc, API: api, Codec: codec, Orders: svc, IsAdmin: cfg.IsAdmin, Admins: cfg.AdminIDs, Notices: st, Langs: st,
		WebAppURL: cfg.WebAppURL(), PayMode: payMode, Log: log,
	})
	if err != nil {
		return err
	}
	svc.Notify = bot
	if live := svc.Live(); !live.Manual && len(live.Providers.IDs()) == 0 && len(cfg.AdminIDs) > 0 {
		bot.NotifyAdmins(ctx, i18n.M("admin.no_methods"), "/admin/methods")
	}
	app := webapp.New(webapp.Deps{
		API: api, Codec: codec, BotToken: cfg.BotToken, BotUsername: bot.Username(),
		Path: cfg.WebAppPath, PayMode: payMode, Orders: svc, Currency: cur.Get, Static: web.Dist, Log: log,
		Settings: set, IsAdmin: cfg.IsAdmin, PublicURL: cfg.PublicURL, Receipts: bot, Unreachable: bot.Unreachable, Langs: st,
	})

	errc := make(chan error, 1)
	go func() { errc <- app.Listen(cfg.HTTPAddr) }()
	go bot.Run(ctx)
	n := &notify.Runner{
		API: api, Store: st, Send: bot.SendNotice, Log: log,
		Conf: func() notify.Conf {
			g := set.Snapshot().General()
			return notify.Conf{Interval: g.NotifyEvery(), Rules: notify.Rules{TrafficPct: g.TrafficAlertPct, RemindDays: g.RemindDescending()}}
		},
	}
	go n.Run(ctx)
	go sweepLoop(ctx, svc, log)
	go providerLoop(ctx, svc, log)
	go currencyLoop(ctx, cur, log, func() bool { return len(svc.Live().Providers.IDs()) > 0 }, svc.CurrencyKnown, unconfirmed)
	log.Info("started", "currency", base, "bot", "@"+bot.Username(), "webapp", cfg.WebAppURL(), "addr", cfg.HTTPAddr)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	log.Info("shutting down")
	return app.Shutdown()
}

// manualOn: transfers need a text to show and an admin to decide.
func manualOn(cfg config.Config, m settings.Manual) bool {
	return m.Active() && len(cfg.AdminIDs) > 0
}

// fallbackCurrency is what is shown while the panel cannot say: the operator's explicit choice, else RUB.
func fallbackCurrency(s settings.Snapshot) string {
	if c := s.General().BaseCurrency; c != "" {
		return c
	}
	return "RUB"
}

// sweepLoop expires stale orders and fails interrupted approvals.
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

// providerLoop recovers lost provider webhooks and expires stale invoices; it also covers retired providers,
// whose invoices may still be paid.
func providerLoop(ctx context.Context, svc *orders.Service, log *slog.Logger) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if err := svc.SweepProviders(ctx); err != nil {
			log.Error("orders: provider sweep", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// logCurrencyUnconfirmed explains why provider payments refuse to invoice.
func logCurrencyUnconfirmed(log *slog.Logger, fallback string) {
	log.Error("provider payments are disabled until the panel reports its base currency: re-issue the bot token with the `stats:settings` scope or set the base currency in the admin settings",
		"fallback", fallback)
}

// currencyLoop keeps the displayed base currency fresh even when no invoice or WebApp request asks for it: Get
// refreshes the Source (whose TTL/backoff decides when the panel is really called) and feeds view.SetCurrency.
// While providers are on it also repeats the "currency not confirmed" error every 10 minutes and says when it
// clears; unconfirmed is the state already reported at startup.
func currencyLoop(ctx context.Context, cur *currency.Source, log *slog.Logger, providers, known func() bool, unconfirmed bool) {
	const repeat = 10 * time.Minute
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		code := cur.Get(ctx)
		if !providers() {
			unconfirmed = false
			continue
		}
		switch {
		case !known():
			if now := time.Now(); !unconfirmed || now.Sub(last) >= repeat {
				logCurrencyUnconfirmed(log, code)
				last = now
			}
			unconfirmed = true
		case unconfirmed:
			unconfirmed = false
			log.Info("provider payments are enabled: the base currency is confirmed", "currency", code)
		}
	}
}
