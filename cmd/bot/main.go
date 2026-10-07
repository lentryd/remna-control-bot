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
	"vpn-control-bot/internal/notify"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/payments"
	"vpn-control-bot/internal/refcode"
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

	api := vpnctl.New(cfg.VpnctlURL, cfg.VpnctlToken)
	cur := &currency.Source{API: api, Fallback: cfg.BaseCurrency, Log: log, OnChange: view.SetCurrency}
	base := cur.Get(ctx) // resolves once at startup: with the panel down the fallback is used and a warning is logged
	codec := refcode.New(cfg.RefSecret)
	acc := access.New(api, codec, cfg.Mode, st)

	svc := &orders.Service{
		DB: st.DB(), Chats: st, API: api, PayMode: cfg.PayMode, Manual: cfg.ManualEnabled(),
		Details: cfg.ManualPayDetails, TTL: cfg.OrderTTL, Now: time.Now, Log: log,
	}
	svc.BaseCurrency = cur.Get
	// Provider invoices need a confirmed currency: the panel answered, or the operator set BASE_CURRENCY explicitly.
	svc.CurrencyKnown = func() bool { return cur.Known() || cfg.BaseCurrencySet }
	var stars *payments.Stars
	var provs []payments.Provider
	pc := cfg.Pay
	if pc.YooKassaOn() {
		provs = append(provs, &payments.YooKassa{ShopID: pc.YooKassaShopID, Secret: pc.YooKassaSecret, ReturnURL: pc.YooKassaReturnURL,
			ReceiptEmail: pc.YooKassaReceiptEmail, VAT: pc.YooKassaVAT})
	}
	if pc.CryptoBotOn() {
		provs = append(provs, &payments.CryptoBot{Token: pc.CryptoBotToken, Testnet: pc.CryptoBotTestnet, Assets: pc.CryptoBotAssets})
	}
	if pc.HeleketOn() {
		provs = append(provs, &payments.Heleket{MerchantID: pc.HeleketMerchantID, APIKey: pc.HeleketAPIKey, ToCurrency: pc.HeleketToCurrency,
			Network: pc.HeleketNetwork, CallbackURL: cfg.PublicURL + "/pay/heleket"})
	}
	if pc.Pal24On() {
		provs = append(provs, &payments.Pal24{APIToken: pc.Pal24APIToken, ShopID: pc.Pal24ShopID, SignatureToken: pc.Pal24SignatureToken})
	}
	if pc.PlategaOn() {
		provs = append(provs, &payments.Platega{MerchantID: pc.PlategaMerchantID, Secret: pc.PlategaSecret, PaymentMethod: pc.PlategaPaymentMethod,
			APIVersion: pc.PlategaAPIVersion, ReturnURL: pc.PlategaReturnURL, FailedURL: pc.PlategaFailedURL})
	}
	if pc.StarsOn() {
		stars = &payments.Stars{PerStar: pc.StarsRate}
		provs = append(provs, stars)
	}
	svc.Providers = payments.NewRegistry(provs...)
	if len(provs) > 0 {
		log.Info("payment providers invoice in the base currency", "currency", base, "providers", svc.Providers.IDs())
	}
	unconfirmed := len(provs) > 0 && !svc.CurrencyKnown()
	if unconfirmed {
		logCurrencyUnconfirmed(log, base)
	}
	if len(provs) > 0 && !strings.HasPrefix(cfg.PublicURL, "https://") {
		log.Warn("payment providers are on but PUBLIC_URL is not https: webhooks will not reach the bot", "url", cfg.PublicURL)
	}
	bot, err := telegram.New(ctx, cfg.BotToken, telegram.Deps{
		Access: acc, API: api, Codec: codec, Orders: svc, Prompts: st, AdminChatID: cfg.AdminChatID, IsAdmin: cfg.IsAdmin,
		WebAppURL: cfg.WebAppURL(), PayMode: cfg.PayMode, Log: log,
	})
	if err != nil {
		return err
	}
	svc.Notify = bot
	if stars != nil {
		stars.Link = bot.InvoiceLink
	}
	if cfg.ManualEnabled() {
		bot.CheckAdminChat(ctx)
	}
	app := webapp.New(webapp.Deps{
		API: api, Codec: codec, BotToken: cfg.BotToken, BotUsername: bot.Username(),
		Path: cfg.WebAppPath, PayMode: cfg.PayMode, Orders: svc, Currency: cur.Get, Static: web.Dist, Log: log,
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
	if len(provs) > 0 {
		go providerLoop(ctx, svc, log)
	}
	go currencyLoop(ctx, cur, log, len(provs) > 0, svc.CurrencyKnown, unconfirmed)
	log.Info("started", "mode", cfg.Mode, "pay", cfg.PayMode, "manual", cfg.ManualEnabled(), "currency", base, "notify", cfg.NotifyInterval, "bot", "@"+bot.Username(), "webapp", cfg.WebAppURL(), "addr", cfg.HTTPAddr, "providers", svc.Providers.IDs())

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

// providerLoop recovers lost provider webhooks and expires stale invoices.
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
	log.Error("provider payments are disabled until the panel reports its base currency: re-issue the bot token with the `stats:settings` scope or set BASE_CURRENCY",
		"fallback", fallback)
}

// currencyLoop keeps the displayed base currency fresh even when no invoice or WebApp request asks for it: Get
// refreshes the Source (whose TTL/backoff decides when the panel is really called) and feeds view.SetCurrency.
// With providers on it also repeats the "currency not confirmed" error every 10 minutes while it holds and says
// when it clears; unconfirmed is the state already reported at startup.
func currencyLoop(ctx context.Context, cur *currency.Source, log *slog.Logger, providers bool, known func() bool, unconfirmed bool) {
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
		if !providers {
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
