// Package webapp serves the Telegram WebApp: its static files and a small
// JSON API authenticated by Telegram initData.
package webapp

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"

	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/profile"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/settings"
	"vpn-control-bot/internal/telegram"
	"vpn-control-bot/internal/vpnctl"
	"vpn-control-bot/internal/webapp/initdata"
)

const maxInitDataAge = 24 * time.Hour

func (d Deps) payMode() config.PayMode {
	if d.PayMode == nil {
		return config.PayMixed
	}
	return d.PayMode()
}

type Deps struct {
	API         vpnctl.Client
	Codec       *refcode.Codec
	BotToken    string
	BotUsername string
	Path        string // "" or "/bot"
	PayMode     func() config.PayMode
	Orders      *orders.Service
	Currency    func(context.Context) string // the base currency amounts are in
	Static      fs.FS
	Log         *slog.Logger
	Settings    *settings.Store
	IsAdmin     func(int64) bool
	PublicURL   string         // the origin providers call back to; shown next to webhook settings
	Receipts    ReceiptSource  // streams receipts from Telegram; nil: receipts are unavailable
	Unreachable func() []int64 // admins the bot could not reach; nil: none
}

func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true, StrictRouting: true, BodyLimit: 1 << 20})
	s := &server{Deps: d}

	app.Post("/pay/:provider", s.payWebhook)

	api := app.Group(d.Path + "/api")
	api.Use(s.initAuth)

	adm := api.Group("/admin", s.adminOnly)
	adm.Get("/me", s.adminMe)
	adm.Get("/orders", s.adminListOrders)
	adm.Get("/orders/:id", s.adminOrder(s.adminGetOrder))
	adm.Post("/orders/:id/approve", s.adminOrder(s.adminApprove))
	adm.Post("/orders/:id/reject", s.adminOrder(s.adminReject))
	adm.Post("/orders/:id/retry", s.adminOrder(s.adminRetry))
	adm.Get("/orders/:id/receipt", s.adminReceipt)
	adm.Get("/settings", s.adminGetSettings)
	adm.Put("/settings/:section", s.adminPutSettings)
	adm.Post("/settings/:section/test", s.adminTestSettings)

	for _, p := range []string{"/me", "/tariffs", "/orders"} {
		api.Use(p, s.customerAuth)
	}
	api.Get("/me", s.me)
	api.Get("/tariffs", s.tariffs)
	api.Use("/orders", s.ordersOn)
	api.Get("/orders", s.listOrders)
	api.Post("/orders/quote", s.quoteOrder)
	api.Post("/orders", s.createOrder)
	api.Post("/orders/:id/receipt", s.orderAction(s.receipt))
	api.Post("/orders/:id/pay-balance", s.orderAction(s.payBalance))
	api.Post("/orders/:id/cancel", s.orderAction(s.cancel))

	if d.Path != "" {
		app.Get(d.Path, func(c *fiber.Ctx) error { return c.Redirect(d.Path+"/", fiber.StatusMovedPermanently) })
	}
	app.Use(d.Path+"/", filesystem.New(filesystem.Config{Root: http.FS(d.Static), Index: "index.html"}))
	return app
}

type server struct{ Deps }

func fail(c *fiber.Ctx, status int, code string) error {
	return c.Status(status).JSON(fiber.Map{"code": code})
}

// initAuth validates the Telegram initData; who the caller is for the bot
// (customer, admin) is decided by the middleware of the route group.
func (s *server) initAuth(c *fiber.Ctx) error {
	raw, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "tma ")
	if !ok {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	u, err := initdata.Validate(raw, s.BotToken, maxInitDataAge, time.Now())
	if err != nil {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	c.Locals("tg", u.ID)
	c.Locals("tguser", u)
	return c.Next()
}

// customerAuth resolves the caller to their vpn-control customer.
func (s *server) customerAuth(c *fiber.Ctx) error {
	cu, err := s.API.LookupByTelegram(c.UserContext(), c.Locals("tg").(int64))
	switch {
	case errors.Is(err, vpnctl.ErrNotFound):
		return fail(c, fiber.StatusForbidden, "no_access")
	case err != nil:
		s.Log.Error("webapp lookup", "err", err)
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	case cu.Archived:
		return fail(c, fiber.StatusForbidden, "no_access")
	}
	c.Locals("customer", cu)
	return c.Next()
}

func (s *server) me(c *fiber.Ctx) error {
	cu := c.Locals("customer").(*vpnctl.Customer)
	p := profile.Build(cu, telegram.RefLink(s.BotUsername, s.Codec, cu.ID), s.payMode())
	p.Currency = "RUB"
	if s.Currency != nil {
		p.Currency = s.Currency(c.UserContext())
	}
	return c.JSON(p)
}

func (s *server) tariffs(c *fiber.Ctx) error {
	ts, err := s.API.ListTariffs(c.UserContext())
	if err != nil {
		s.Log.Error("webapp tariffs", "err", err)
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	return c.JSON(profile.Tariffs(ts))
}
