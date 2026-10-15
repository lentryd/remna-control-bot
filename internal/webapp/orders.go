package webapp

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/remnactl"
)

type orderView struct {
	ID          string     `json:"id"`
	Number      string     `json:"number"` // short form to show people
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Price       float64    `json:"price"`
	FromBalance float64    `json:"from_balance"`
	ToPay       float64    `json:"to_pay"`
	Paid        float64    `json:"paid"`
	Method      string     `json:"method"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason,omitempty"`
	Until       *time.Time `json:"until,omitempty"`
	PayURL      string     `json:"pay_url,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func orderJSON(l i18n.Localizer, o orders.Order) orderView {
	return orderView{ID: o.ID, Number: o.Short(), Kind: string(o.Kind), Title: l.Text(o.Title), Price: o.Price, FromBalance: o.FromBalance, ToPay: o.ToPay,
		Paid: o.Paid(), Method: string(o.Method), Status: string(o.Status), Reason: o.RejectReason,
		Until: o.ResultUntil, PayURL: o.PayURL, CreatedAt: o.CreatedAt}
}

// ordersOn answers 503 while manual payments are not wired in.
func (s *server) ordersOn(c *fiber.Ctx) error {
	if s.Orders == nil {
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	return c.Next()
}

func (s *server) orderErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, orders.ErrBadRequest):
		return fail(c, fiber.StatusBadRequest, "bad_request")
	case errors.Is(err, orders.ErrNotFound):
		return fail(c, fiber.StatusNotFound, "not_found")
	case errors.Is(err, orders.ErrStateChanged):
		return fail(c, fiber.StatusConflict, "state_changed")
	case errors.Is(err, orders.ErrManualOff):
		return fail(c, fiber.StatusConflict, "manual_off")
	case errors.Is(err, orders.ErrUnderpaid):
		return fail(c, fiber.StatusConflict, "underpaid")
	case errors.Is(err, orders.ErrProvider):
		s.Log.Warn("webapp orders", "err", err)
		return fail(c, fiber.StatusFailedDependency, "provider_failed")
	}
	s.Log.Error("webapp orders", "err", err)
	return fail(c, fiber.StatusServiceUnavailable, "unavailable")
}

func (s *server) quoteOrder(c *fiber.Ctx) error {
	var r orders.Request
	if err := c.BodyParser(&r); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	q, err := s.Orders.Quote(c.UserContext(), c.Locals("customer").(*remnactl.Customer), r)
	if err != nil {
		return s.orderErr(c, err)
	}
	l := loc(c)
	q.Title = l.Text(q.Title)
	q.Titles = quoteTitles(l, q.Titles)
	return c.JSON(q)
}

// quoteTitles names payment providers in the reader's language (payment.<id>), else by their own title.
func quoteTitles(l i18n.Localizer, titles map[string]string) map[string]string {
	if titles == nil {
		return nil
	}
	out := make(map[string]string, len(titles))
	for id, own := range titles {
		out[id] = l.Or("payment."+id, own)
	}
	return out
}

func (s *server) createOrder(c *fiber.Ctx) error {
	var r struct {
		orders.Request
		Method string `json:"method"`
	}
	if err := c.BodyParser(&r); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	o, err := s.Orders.CreateWith(c.UserContext(), c.Locals("customer").(*remnactl.Customer), c.Locals("tg").(int64), r.Request, orders.Method(r.Method))
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"order": orderJSON(loc(c), *o), "details": s.Orders.Live().Details})
}

func (s *server) listOrders(c *fiber.Ctx) error {
	page := min(max(c.QueryInt("page", 0), 0), 10000)
	list, total, err := s.Orders.List(c.UserContext(), c.Locals("customer").(*remnactl.Customer).ID, 20, page*20)
	if err != nil {
		return s.orderErr(c, err)
	}
	l := loc(c)
	out := make([]orderView, 0, len(list))
	for _, o := range list {
		out = append(out, orderJSON(l, o))
	}
	return c.JSON(fiber.Map{"orders": out, "total": total})
}

// orderAction resolves :id for the caller's customer and runs fn.
func (s *server) orderAction(fn func(c *fiber.Ctx, customerID int, id string) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
			return fail(c, fiber.StatusNotFound, "not_found")
		}
		return fn(c, c.Locals("customer").(*remnactl.Customer).ID, id)
	}
}

func (s *server) receipt(c *fiber.Ctx, customerID int, id string) error {
	if _, err := s.Orders.RequestReceipt(c.UserContext(), customerID, id); err != nil {
		return s.orderErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// payBalance returns the order as it ended up: a panel failure comes back
// as a "failed" order, not as an HTTP error. Order.Error (raw panel text) is
// never exposed to the customer.
func (s *server) payBalance(c *fiber.Ctx, customerID int, id string) error {
	o, err := s.Orders.PayFromBalance(c.UserContext(), customerID, id)
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.JSON(orderJSON(loc(c), *o))
}

func (s *server) cancel(c *fiber.Ctx, customerID int, id string) error {
	if err := s.Orders.Cancel(c.UserContext(), customerID, id); err != nil {
		return s.orderErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
