package webapp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/payments"
	"remna-control-bot/internal/settings"
	"remna-control-bot/internal/webapp/initdata"
)

// ReceiptSource streams a receipt file kept at Telegram (*telegram.Bot).
type ReceiptSource interface {
	Receipt(ctx context.Context, fileID string) (io.ReadCloser, string, error)
	// SendReceipt forwards the file to a chat, for viewing it in Telegram itself.
	SendReceipt(ctx context.Context, chatID int64, kind orders.ReceiptKind, fileID string) error
}

const adminPage = 20

func adminName(u initdata.User) string {
	id := strconv.FormatInt(u.ID, 10)
	if u.Username != "" {
		return "@" + u.Username + " (" + id + ")"
	}
	if n := strings.TrimSpace(u.FirstName + " " + u.LastName); n != "" {
		return n + " (" + id + ")"
	}
	return id
}

// adminOnly lets through the Telegram ids listed in ADMIN_IDS; they need not be panel customers.
func (s *server) adminOnly(c *fiber.Ctx) error {
	u, _ := c.Locals("tguser").(initdata.User)
	if s.IsAdmin == nil || u.ID == 0 || !s.IsAdmin(u.ID) {
		return fail(c, fiber.StatusForbidden, "forbidden")
	}
	c.Locals("admin", adminName(u))
	return c.Next()
}

func (s *server) adminMe(c *fiber.Ctx) error {
	u, ok := c.Locals("tguser").(initdata.User)
	if !ok {
		return fail(c, fiber.StatusUnauthorized, "unauthorized")
	}
	cur := "RUB"
	if s.Currency != nil {
		cur = s.Currency(c.UserContext())
	}
	return c.JSON(fiber.Map{"id": u.ID, "name": adminName(u), "currency": cur})
}

type adminOrderView struct {
	orderView
	CustomerID   int      `json:"customer_id"`
	CustomerName string   `json:"customer_name,omitempty"`
	TelegramID   int64    `json:"telegram_id"`
	Comment      string   `json:"comment,omitempty"`
	ReceiptKind  string   `json:"receipt_kind,omitempty"`
	Received     *float64 `json:"received"`
	Admin        string   `json:"admin,omitempty"`
	Decision     string   `json:"decision,omitempty"`
	Error        string   `json:"error,omitempty"`
	ExternalID   string   `json:"external_id,omitempty"`
	PayAmount    float64  `json:"pay_amount,omitempty"`
	PayCurrency  string   `json:"pay_currency,omitempty"`
}

func adminOrderJSON(l i18n.Localizer, o orders.Order, customerName string) adminOrderView {
	return adminOrderView{
		orderView: orderJSON(l, o), CustomerID: o.CustomerID, CustomerName: customerName, TelegramID: o.ChatID,
		Comment: l.Text(o.Comment), ReceiptKind: string(o.ReceiptKind), Received: o.Received, Admin: o.Admin,
		Decision: string(o.Decision), Error: l.Text(o.Error), ExternalID: o.ExternalID, PayAmount: o.PayAmount, PayCurrency: o.PayCurrency,
	}
}

func (s *server) adminListOrders(c *fiber.Ctx) error {
	var statuses []orders.Status
	if q := c.Query("status"); q != "" && q != "all" {
		for _, v := range strings.Split(q, ",") {
			st := orders.Status(strings.TrimSpace(v))
			if !st.Valid() {
				return fail(c, fiber.StatusBadRequest, "bad_request")
			}
			statuses = append(statuses, st)
		}
	}
	page := min(max(c.QueryInt("page", 0), 0), 10000)
	list, total, err := s.Orders.ListAll(c.UserContext(), statuses, adminPage, page*adminPage)
	if err != nil {
		return s.orderErr(c, err)
	}
	l := loc(c)
	out := make([]adminOrderView, 0, len(list))
	for _, o := range list {
		out = append(out, adminOrderJSON(l, o, ""))
	}
	unreachable := []int64{}
	if s.Unreachable != nil {
		unreachable = append(unreachable, s.Unreachable()...)
	}
	return c.JSON(fiber.Map{"orders": out, "total": total, "unreachable_admins": unreachable})
}

// adminOrder checks :id and turns fn's result into the admin order JSON.
func (s *server) adminOrder(fn func(c *fiber.Ctx, id, admin string) (*orders.Order, error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
			return fail(c, fiber.StatusNotFound, "not_found")
		}
		o, err := fn(c, id, c.Locals("admin").(string))
		if err != nil {
			return s.orderErr(c, err)
		}
		name := ""
		if cu, err := s.API.GetCustomer(c.UserContext(), o.CustomerID); err == nil {
			name = cu.Name
		}
		return c.JSON(adminOrderJSON(loc(c), *o, name))
	}
}

func (s *server) adminGetOrder(c *fiber.Ctx, id, _ string) (*orders.Order, error) {
	return s.Orders.Get(c.UserContext(), id)
}

func (s *server) adminApprove(c *fiber.Ctx, id, admin string) (*orders.Order, error) {
	var b struct {
		Decision string   `json:"decision"`
		Amount   *float64 `json:"amount"`
	}
	if err := c.BodyParser(&b); err != nil {
		return nil, fmt.Errorf("%w: body", orders.ErrBadRequest)
	}
	switch d := orders.Decision(b.Decision); d {
	case orders.DecideFull, orders.DecideDebt, orders.DecideCredit:
	default:
		return nil, fmt.Errorf("%w: decision", orders.ErrBadRequest)
	}
	if b.Amount != nil {
		if _, err := s.Orders.SetReceived(c.UserContext(), id, *b.Amount, admin); err != nil {
			return nil, err
		}
	}
	return s.Orders.Approve(c.UserContext(), id, admin, orders.Decision(b.Decision))
}

func (s *server) adminReject(c *fiber.Ctx, id, admin string) (*orders.Order, error) {
	var b struct {
		Reason string `json:"reason"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&b); err != nil {
			return nil, fmt.Errorf("%w: body", orders.ErrBadRequest)
		}
	}
	return s.Orders.Reject(c.UserContext(), id, b.Reason, admin)
}

func (s *server) adminRetry(c *fiber.Ctx, id, admin string) (*orders.Order, error) {
	return s.Orders.Retry(c.UserContext(), id, admin)
}

// adminReceipt streams the customer's receipt file. The stream is closed by the
// HTTP server after the response is written, so it must not be closed here.
func (s *server) adminReceipt(c *fiber.Ctx) error {
	o, ok, err := s.receiptOrder(c)
	if !ok {
		return err
	}
	rc, _, err := s.Receipts.Receipt(c.UserContext(), o.ReceiptFileID)
	if err != nil {
		s.Log.Warn("webapp receipt", "order", o.ID, "err", err)
		return fail(c, fiber.StatusBadGateway, "unavailable")
	}
	// Telegram's file server labels documents as it likes, so the type comes from the bytes.
	br := bufio.NewReaderSize(rc, 512)
	head, _ := br.Peek(512)
	ctype, attachment := receiptHeaders(o.ReceiptKind, head)
	body := struct {
		io.Reader
		io.Closer
	}{br, rc}
	c.Set(fiber.HeaderContentType, ctype)
	c.Set("X-Content-Type-Options", "nosniff")
	if attachment {
		c.Set(fiber.HeaderContentDisposition, "attachment")
	}
	c.Set(fiber.HeaderCacheControl, "private, max-age=300")
	return c.SendStream(body)
}

// receiptOrder finds the order of the request and checks it has a receipt file
// and that files can be fetched. ok=false: the answer has been written (err is its result).
func (s *server) receiptOrder(c *fiber.Ctx) (o *orders.Order, ok bool, err error) {
	id := c.Params("id")
	if _, perr := uuid.Parse(id); perr != nil || len(id) != 36 {
		return nil, false, fail(c, fiber.StatusNotFound, "not_found")
	}
	o, err = s.Orders.Get(c.UserContext(), id)
	if err != nil {
		return nil, false, s.orderErr(c, err)
	}
	if o.ReceiptFileID == "" || (o.ReceiptKind != orders.ReceiptPhoto && o.ReceiptKind != orders.ReceiptDocument) {
		return nil, false, fail(c, fiber.StatusNotFound, "not_found")
	}
	if s.Receipts == nil {
		return nil, false, fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	return o, true, nil
}

// adminSendReceipt sends the receipt file to the admin's own chat with the bot,
// where Telegram opens any file type.
func (s *server) adminSendReceipt(c *fiber.Ctx) error {
	o, ok, err := s.receiptOrder(c)
	if !ok {
		return err
	}
	u, _ := c.Locals("tguser").(initdata.User)
	if err := s.Receipts.SendReceipt(c.UserContext(), u.ID, o.ReceiptKind, o.ReceiptFileID); err != nil {
		s.Log.Warn("webapp send receipt", "order", o.ID, "err", err)
		return fail(c, fiber.StatusBadGateway, "unavailable")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// receiptHeaders decides how a receipt is served, by the first bytes of the file.
// A document is customer-uploaded content, so only a few inert types are shown
// inline; the rest is a download.
func receiptHeaders(kind orders.ReceiptKind, head []byte) (ctype string, attachment bool) {
	if kind == orders.ReceiptPhoto {
		return "image/jpeg", false // Telegram photos are JPEG whatever its file server says
	}
	mt, _, _ := strings.Cut(http.DetectContentType(head), ";")
	switch mt {
	case "application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp":
		return mt, false
	}
	return "application/octet-stream", true
}

func (s *server) adminGetSettings(c *fiber.Ctx) error {
	sections := make(map[string]settings.Section, len(settings.Keys))
	for _, k := range settings.Keys {
		v, _ := s.Settings.View(k)
		sections[k] = v
	}
	out := fiber.Map{"sections": sections, "lost": s.Settings.Lost(), "webhook_base": s.PublicURL}
	if s.Profile != nil {
		out["profile_defaults"] = s.Profile.ProfileDefaults()
	}
	return c.JSON(out)
}

func (s *server) adminPutSettings(c *fiber.Ctx) error {
	key := c.Params("section")
	body := c.Body()
	if key == settings.KeyProfile && s.Profile != nil {
		body = keepOverrides(body, s.Profile.ProfileDefaults())
	}
	err := s.Settings.Put(c.UserContext(), key, body)
	var ve *settings.ValidationError
	switch {
	case errors.Is(err, settings.ErrUnknownSection):
		return fail(c, fiber.StatusNotFound, "not_found")
	case errors.As(err, &ve):
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"code": "invalid", "message": ve.Msg})
	case err != nil:
		s.Log.Error("webapp settings", "section", key, "err", err)
		return fail(c, fiber.StatusServiceUnavailable, "unavailable")
	}
	v, _ := s.Settings.View(key)
	if p, ok := v.(*settings.Profile); ok && s.Profile != nil {
		// The texts are saved whatever Telegram says; the answer tells the admin what it did not take.
		ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
		defer cancel()
		return c.JSON(fiber.Map{"texts": p.Texts, "apply": s.Profile.ApplyProfile(ctx)})
	}
	return c.JSON(v)
}

// keepOverrides drops the profile texts that equal the built-in ones: the form
// shows the built-in text in each field, so only a text that differs is an
// override. A body it can't read is left for Put to refuse.
func keepOverrides(raw []byte, builtin map[string]settings.ProfileTexts) []byte {
	var p settings.Profile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil {
		return raw
	}
	keep := func(v, def string) string {
		if v = strings.TrimSpace(v); v == strings.TrimSpace(def) {
			return ""
		}
		return v
	}
	for lang, t := range p.Texts {
		b := builtin[lang]
		p.Texts[lang] = settings.ProfileTexts{Name: keep(t.Name, b.Name), Description: keep(t.Description, b.Description), About: keep(t.About, b.About)}
	}
	out, err := json.Marshal(p)
	if err != nil {
		return raw
	}
	return out
}

// adminTestSettings asks the provider of a saved, active payment section to
// prove its credentials. Sections without a provider have nothing to test.
func (s *server) adminTestSettings(c *fiber.Ctx) error {
	key := c.Params("section")
	id, ok := strings.CutPrefix(key, "pay.")
	if !ok || id == "manual" {
		return fail(c, fiber.StatusBadRequest, "bad_request")
	}
	p, ok := s.Orders.Live().Providers.Offer(id)
	if !ok {
		return fail(c, fiber.StatusConflict, "not_active")
	}
	ck, ok := p.(payments.Checker)
	if !ok {
		return c.JSON(fiber.Map{"ok": false, "supported": false})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	msg, err := ck.Check(ctx)
	if err != nil {
		return c.JSON(fiber.Map{"ok": false, "supported": true, "message": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "supported": true, "message": msg})
}
