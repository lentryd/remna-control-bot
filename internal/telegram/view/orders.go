package view

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/orders"
)

var statusIcon = map[orders.Status]string{
	orders.StatusAwaitingReceipt: "📎", orders.StatusAwaitingPayment: "💳", orders.StatusPendingReview: "⏳", orders.StatusApproving: "⏳",
	orders.StatusApproved: "✅", orders.StatusCredited: "💰", orders.StatusFailed: "⚠️",
	orders.StatusRejected: "❌", orders.StatusExpired: "⌛", orders.StatusCancelled: "✖️",
}

// statusText is the one-line state of an order ("" for a status without a text).
func statusText(loc i18n.Localizer, s orders.Status) string {
	if k := "status." + string(s); loc.Has(k) {
		return loc.T(k)
	}
	return ""
}

func id(prefix string, n int) string { return prefix + strconv.Itoa(n) }

// summary: what is bought and how it is paid.
func summary(loc i18n.Localizer, o orders.Order) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n", loc.T("order.head", o.Short()), Esc(clip(loc.Text(o.Title), maxTitle)))
	b.WriteString(loc.T("order.price", Money(loc, o.Price)) + "\n")
	if o.FromBalance > 0 {
		b.WriteString(loc.T("order.from_balance", Money(loc, o.FromBalance)) + "\n")
	}
	b.WriteString(loc.T("order.to_pay", Money(loc, o.ToPay)))
	return b.String()
}

func PayScreen(loc i18n.Localizer, o orders.Order, details string) Screen {
	text := summary(loc, o) + "\n\n" + loc.T("order.how_to_pay") + "\n" + Esc(details) + "\n\n" + loc.T("msg.receipt_hint")
	return Screen{Text: text, Keyboard: kb([]btn{cb(loc.T("btn.cancel"), CbOrderCancel+o.ID)})}
}

// ProviderButton is an online payment method offered on the confirm screen.
type ProviderButton struct{ ID, Title string }

// Confirm is the pay screen before payment is chosen (bot renew flow): a
// receipt for manual orders, one button per provider, cancel.
func Confirm(loc i18n.Localizer, o orders.Order, providers ...ProviderButton) Screen {
	if o.Method == orders.MethodBalance {
		return Screen{Text: summary(loc, o) + "\n\n" + loc.T("order.balance_all"), Keyboard: kb(
			[]btn{cb(loc.T("btn.pay_balance"), CbOrderBal+o.ID)}, []btn{cb(loc.T("btn.cancel"), CbOrderCancel+o.ID)})}
	}
	var rows [][]btn
	if o.Method == orders.MethodManual {
		rows = append(rows, []btn{cb(loc.T("btn.send_receipt"), CbOrderPay+o.ID)})
	}
	for _, p := range providers {
		rows = append(rows, []btn{cb("💳 "+providerTitle(loc, p.ID, p.Title), CbOrderProvider+p.ID+":"+o.ID)})
	}
	rows = append(rows, []btn{cb(loc.T("btn.cancel"), CbOrderCancel+o.ID)})
	return Screen{Text: summary(loc, o), Keyboard: kb(rows...)}
}

// providerTitle names a payment provider in the reader's language (payment.<id>), else by its own title.
func providerTitle(loc i18n.Localizer, id, fallback string) string {
	return loc.Or("payment."+id, fallback)
}

// ProviderPay carries the payment link of a provider order; title is the provider's own name, used when the
// catalog has none for o.Method.
func ProviderPay(loc i18n.Localizer, o orders.Order, title string) Screen {
	text := summary(loc, o) + "\n\n" + loc.T("order.pay_with", Esc(providerTitle(loc, string(o.Method), title)))
	return Screen{Text: text, Keyboard: kb([]btn{{Text: loc.T("btn.pay"), URL: o.PayURL}}, []btn{cb(loc.T("btn.cancel"), CbOrderCancel+o.ID)})}
}

type RenewOption struct {
	Months, Days int
	Price        float64
}

func RenewPeriods(loc i18n.Localizer, subID int, title string, opts []RenewOption) Screen {
	var rows [][]btn
	for _, o := range opts {
		label := fmt.Sprintf("%s — %s", termText(loc, o.Months, o.Days), Money(loc, o.Price))
		rows = append(rows, []btn{cb(label, fmt.Sprintf("%s%d:%d:%d", CbRenewPick, subID, o.Months, o.Days))})
	}
	rows = append(rows, back(loc, id(CbSub, subID)))
	return Screen{Text: loc.T("msg.pick_period", Esc(clip(title, maxTitle))), Keyboard: kb(rows...)}
}

func termText(loc i18n.Localizer, months, days int) string {
	var p []string
	if months > 0 {
		p = append(p, loc.T("term.months", months))
	}
	if days > 0 {
		p = append(p, loc.T("term.days", days))
	}
	return strings.Join(p, " ")
}

// AdminCustomer is who sent the receipt, as the admins see them.
type AdminCustomer struct {
	Name       string
	Username   string
	TelegramID int64
}

var providerTitles = map[string]string{
	"cryptobot": "CryptoBot", "heleket": "Heleket",
	"pal24": "PayPalych", "platega": "Platega", "stars": "Telegram Stars",
}

// methodTitle says how an order was paid, for the admin card.
func methodTitle(loc i18n.Localizer, m orders.Method) string {
	switch {
	case m == "":
		return ""
	case m == orders.MethodManual:
		return loc.T("method.manual")
	case m == orders.MethodBalance:
		return loc.T("method.balance")
	}
	if m == "yookassa" {
		return loc.T("provider.yookassa")
	}
	if t, ok := providerTitles[string(m)]; ok {
		return t
	}
	return string(m)
}

// AdminCard is the notification an admin gets in their chat: what the order is
// and where it stands, with a button into the admin part of the WebApp where the
// decision is made (receipts are viewed there).
func AdminCard(loc i18n.Localizer, o orders.Order, c AdminCustomer, open *models.WebAppInfo) Screen {
	var b strings.Builder
	b.WriteString(summary(loc, o))
	who := Esc(clip(c.Name, 40))
	if c.Username != "" {
		who += " @" + Esc(c.Username)
	}
	b.WriteString("\n\n" + loc.T("admin.customer", who, o.CustomerID))
	if m := methodTitle(loc, o.Method); m != "" {
		b.WriteString("\n" + loc.T("admin.method", Esc(m)))
	}
	if o.Comment != "" {
		fmt.Fprintf(&b, "\n💬 %s", Esc(clip(loc.Text(o.Comment), 300)))
	}
	if o.ReceiptKind == orders.ReceiptPhoto || o.ReceiptKind == orders.ReceiptDocument {
		b.WriteString("\n" + loc.T("admin.receipt"))
	}
	if o.Received != nil {
		b.WriteString("\n" + loc.T("admin.amount_fixed", Esc(o.Admin), plain(loc, o.ToPay), Money(loc, *o.Received)))
	}
	switch o.Status {
	case orders.StatusPendingReview:
		b.WriteString("\n\n" + loc.T("admin.pending"))
	case orders.StatusApproved:
		b.WriteString("\n\n" + loc.T("admin.approved", Esc(o.Admin)))
		if o.ResultUntil != nil {
			b.WriteString(loc.T("admin.valid_until", Date(loc, o.ResultUntil)))
		}
	case orders.StatusCredited:
		b.WriteString("\n\n" + loc.T("admin.credited", Esc(o.Admin)))
	case orders.StatusRejected:
		b.WriteString("\n\n" + loc.T("admin.rejected", Esc(o.Admin)))
		if o.RejectReason != "" {
			fmt.Fprintf(&b, ": %s", Esc(clip(o.RejectReason, 200)))
		}
	case orders.StatusFailed:
		b.WriteString("\n\n" + loc.T("admin.failed", Esc(clip(loc.Text(o.Error), 200))))
	default:
		fmt.Fprintf(&b, "\n\n%s %s", statusIcon[o.Status], statusText(loc, o.Status))
	}
	return Screen{Text: b.String(), Keyboard: kb([]btn{{Text: loc.T("btn.open_admin"), WebApp: open}})}
}

// AdminEntry is a message with a button into a page of the admin part.
func AdminEntry(loc i18n.Localizer, text string, open *models.WebAppInfo) Screen {
	return Screen{Text: text, Keyboard: kb([]btn{{Text: loc.T("btn.open_admin"), WebApp: open}})}
}

// plain is an amount without the currency (for "700 → 500 ₽").
func plain(loc i18n.Localizer, v float64) string {
	return strings.TrimSuffix(Money(loc, v), " "+suffix())
}

// OrderNotice tells the customer what was decided.
func OrderNotice(loc i18n.Localizer, o orders.Order) Screen {
	var text string
	switch o.Status {
	case orders.StatusApproved:
		text = loc.T("order.approved", o.Short(), Esc(loc.Text(o.Title)))
		if o.ResultUntil != nil {
			text += loc.T("order.valid_until", Date(loc, o.ResultUntil))
		}
	case orders.StatusCredited:
		text = loc.T("order.credited", o.Short(), Money(loc, o.Paid()), Money(loc, o.ToPay))
	case orders.StatusRejected:
		text = loc.T("order.rejected", o.Short())
		if o.RejectReason != "" {
			text += ": " + Esc(clip(o.RejectReason, 200))
		}
	case orders.StatusFailed:
		text = loc.T("order.failed", o.Short())
	default:
		text = loc.T("order.status_line", statusIcon[o.Status], o.Short(), statusText(loc, o.Status))
	}
	if o.Received != nil && o.Status == orders.StatusApproved && *o.Received != o.ToPay {
		text += "\n" + loc.T("order.received", Money(loc, *o.Received))
	}
	return Screen{Text: text, Keyboard: kb([]btn{cb(loc.T("btn.orders"), CbOrders)})}
}

const ordersPerPage = 10

func Orders(loc i18n.Localizer, list []orders.Order, n, total int) Screen {
	if total == 0 {
		return Screen{Text: loc.T("orders.empty"), Keyboard: kb(back(loc, CbHome))}
	}
	cur, pages, _, _ := page(n, total, ordersPerPage)
	var b strings.Builder
	b.WriteString(loc.T("orders.title") + pageTitle(loc, cur, pages) + "\n")
	for _, o := range list {
		b.WriteString("\n" + loc.T("orders.item", statusIcon[o.Status], o.Short(), Date(loc, &o.CreatedAt), Money(loc, o.Paid()),
			Esc(clip(loc.Text(o.Title), maxTitle)), statusText(loc, o.Status)))
	}
	rows := [][]btn{}
	if nav := pager(loc, CbOrdersPage, cur, pages); nav != nil {
		rows = append(rows, nav)
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows, back(loc, CbHome))...)}
}
