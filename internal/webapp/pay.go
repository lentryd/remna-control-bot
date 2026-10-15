package webapp

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/payments"
)

const maxHookBody = 64 << 10

// payWebhook receives a provider's callback. Providers retry anything but a
// 200, so handled, duplicate, unknown and irrelevant events all answer 200;
// 500 is only for failures a retry can fix.
func (s *server) payWebhook(c *fiber.Ctx) error {
	if s.Orders == nil {
		return fail(c, fiber.StatusNotFound, "not_found")
	}
	p, ok := s.Orders.Live().Providers.Get(c.Params("provider"))
	wp, hook := p.(payments.WebhookProvider)
	if !ok || !hook {
		return fail(c, fiber.StatusNotFound, "not_found")
	}
	body := c.Body()
	if len(body) > maxHookBody {
		return fail(c, fiber.StatusRequestEntityTooLarge, "too_large")
	}
	h := http.Header{}
	c.Request().Header.VisitAll(func(k, v []byte) { h.Add(string(k), string(v)) })

	ev, relevant, err := wp.Webhook(c.UserContext(), h, body)
	switch {
	case errors.Is(err, payments.ErrBadSignature):
		s.Log.Warn("pay webhook: bad signature", "provider", p.ID())
		return fail(c, fiber.StatusUnauthorized, "bad_signature")
	case errors.Is(err, payments.ErrBadBody):
		return fail(c, fiber.StatusBadRequest, "bad_request")
	case err != nil:
		s.Log.Error("pay webhook", "provider", p.ID(), "err", err)
		return fail(c, fiber.StatusInternalServerError, "unavailable")
	case !relevant:
		return c.SendStatus(fiber.StatusOK)
	}
	if _, err := s.Orders.Paid(c.UserContext(), ev); err != nil {
		switch {
		case errors.Is(err, orders.ErrNotFound):
			// Paid has already alerted the admins about it.
			s.Log.Error("pay webhook: paid event for an unknown order", "provider", p.ID(), "order", ev.OrderID, "external_id", ev.ExternalID, "err", err)
			return c.SendStatus(fiber.StatusOK)
		case errors.Is(err, orders.ErrStateChanged):
			s.Log.Info("pay webhook: nothing to do", "provider", p.ID(), "order", ev.OrderID, "reason", err)
			return c.SendStatus(fiber.StatusOK)
		}
		s.Log.Error("pay webhook: apply", "provider", p.ID(), "order", ev.OrderID, "err", err)
		return fail(c, fiber.StatusInternalServerError, "unavailable")
	}
	return c.SendStatus(fiber.StatusOK)
}
