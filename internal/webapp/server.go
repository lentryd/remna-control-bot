// Package webapp serves the Telegram WebApp: its static files and a small
// JSON API authenticated by Telegram initData.
package webapp

import (
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
	"vpn-control-bot/internal/telegram"
	"vpn-control-bot/internal/vpnctl"
	"vpn-control-bot/internal/webapp/initdata"
)

const maxInitDataAge = 24 * time.Hour

type Deps struct {
	API         vpnctl.Client
	Codec       *refcode.Codec
	BotToken    string
	BotUsername string
	Path        string // "" or "/bot"
	PayMode     config.PayMode
	Orders      *orders.Service
	Static      fs.FS
	Log         *slog.Logger
}

func New(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true, StrictRouting: true})
	s := &server{Deps: d}

	api := app.Group(d.Path + "/api")
	api.Use(s.auth)
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

// auth resolves the caller to their vpn-control customer.
func (s *server) auth(c *fiber.Ctx) error {
	raw, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "tma ")
	if !ok {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	u, err := initdata.Validate(raw, s.BotToken, maxInitDataAge, time.Now())
	if err != nil {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	cu, err := s.API.LookupByTelegram(c.UserContext(), u.ID)
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
	c.Locals("tg", u.ID)
	return c.Next()
}

func (s *server) me(c *fiber.Ctx) error {
	cu := c.Locals("customer").(*vpnctl.Customer)
	return c.JSON(profile.Build(cu, telegram.RefLink(s.BotUsername, s.Codec, cu.ID), s.PayMode))
}

func (s *server) tariffs(c *fiber.Ctx) error {
	ts, err := s.API.ListTariffs(c.UserContext())
	if err != nil {
		s.Log.Error("webapp tariffs", "err", err)
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	return c.JSON(profile.Tariffs(ts))
}
