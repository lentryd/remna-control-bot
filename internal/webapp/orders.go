package webapp

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/vpnctl"
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
	CreatedAt   time.Time  `json:"created_at"`
}

func orderJSON(o orders.Order) orderView {
	return orderView{ID: o.ID, Number: o.Short(), Kind: string(o.Kind), Title: o.Title, Price: o.Price, FromBalance: o.FromBalance, ToPay: o.ToPay,
		Paid: o.Paid(), Method: string(o.Method), Status: string(o.Status), Reason: o.RejectReason,
		Until: o.ResultUntil, CreatedAt: o.CreatedAt}
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
	}
	s.Log.Error("webapp orders", "err", err)
	return fail(c, fiber.StatusServiceUnavailable, "unavailable")
}

func (s *server) quoteOrder(c *fiber.Ctx) error {
	var r orders.Request
	if err := c.BodyParser(&r); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	q, err := s.Orders.Quote(c.UserContext(), c.Locals("customer").(*vpnctl.Customer), r)
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.JSON(q)
}

func (s *server) createOrder(c *fiber.Ctx) error {
	var r orders.Request
	if err := c.BodyParser(&r); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	o, err := s.Orders.Create(c.UserContext(), c.Locals("customer").(*vpnctl.Customer), c.Locals("tg").(int64), r)
	if err != nil {
		return s.orderErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"order": orderJSON(*o), "details": s.Orders.Details})
}

func (s *server) listOrders(c *fiber.Ctx) error {
	page := min(max(c.QueryInt("page", 0), 0), 10000)
	list, total, err := s.Orders.List(c.UserContext(), c.Locals("customer").(*vpnctl.Customer).ID, 20, page*20)
	if err != nil {
		return s.orderErr(c, err)
	}
	out := make([]orderView, 0, len(list))
	for _, o := range list {
		out = append(out, orderJSON(o))
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
		return fn(c, c.Locals("customer").(*vpnctl.Customer).ID, id)
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
	return c.JSON(orderJSON(*o))
}

func (s *server) cancel(c *fiber.Ctx, customerID int, id string) error {
	if err := s.Orders.Cancel(c.UserContext(), customerID, id); err != nil {
		return s.orderErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
