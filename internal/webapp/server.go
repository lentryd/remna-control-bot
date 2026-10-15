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

	"remna-control-bot/internal/config"
	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/profile"
	"remna-control-bot/internal/refcode"
	"remna-control-bot/internal/remnactl"
	"remna-control-bot/internal/settings"
	"remna-control-bot/internal/telegram"
	"remna-control-bot/internal/webapp/initdata"
)

const maxInitDataAge = 24 * time.Hour

func (d Deps) payMode() config.PayMode {
	if d.PayMode == nil {
		return config.PayMixed
	}
	return d.PayMode()
}

type Deps struct {
	API         remnactl.Client
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
	Langs       LangStore      // remembers the language of people who open the app; nil: not remembered
	Profile     ProfileApplier // pushes saved profile texts to Telegram; nil: they are only stored
}

// ProfileApplier sends the bot's saved profile texts to Telegram.
type ProfileApplier interface {
	ApplyProfile(ctx context.Context) settings.ProfileApply
	ProfileDefaults() map[string]settings.ProfileTexts // the built-in texts per language, for the form to show
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

// LangStore remembers each person's Telegram language for messages the bot sends on its own (*store.Store).
type LangStore interface {
	SetLang(ctx context.Context, tgID int64, lang string) error
}

type server struct{ Deps }

// loc is the language of the person making the request (English when Telegram reported none we have).
func loc(c *fiber.Ctx) i18n.Localizer {
	l, ok := c.Locals("loc").(i18n.Localizer)
	if !ok {
		return i18n.Default.For(i18n.Base)
	}
	return l
}

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
	l := i18n.Default.For(u.LanguageCode)
	c.Locals("loc", l)
	// Order and invoice texts are rendered from the request context, so it carries the payer's language too.
	c.SetUserContext(i18n.NewContext(c.UserContext(), l))
	// A missing code stores nothing, so it never overwrites a remembered language; a failure to remember
	// one must never fail the request.
	if s.Langs != nil && strings.TrimSpace(u.LanguageCode) != "" {
		if err := s.Langs.SetLang(c.UserContext(), u.ID, i18n.Default.Resolve(u.LanguageCode)); err != nil && s.Log != nil {
			s.Log.Warn("webapp remember language", "err", err)
		}
	}
	return c.Next()
}

// customerAuth resolves the caller to their remna-control customer.
func (s *server) customerAuth(c *fiber.Ctx) error {
	cu, err := s.API.LookupByTelegram(c.UserContext(), c.Locals("tg").(int64))
	switch {
	case errors.Is(err, remnactl.ErrNotFound):
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
	cu := c.Locals("customer").(*remnactl.Customer)
	p := profile.Build(cu, telegram.RefLink(s.BotUsername, s.Codec, cu.ID), s.payMode())
	p.Currency = "RUB"
	if s.Currency != nil {
		p.Currency = s.Currency(c.UserContext())
	}
	p.IsAdmin = s.IsAdmin != nil && s.IsAdmin(c.Locals("tg").(int64))
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
