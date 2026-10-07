package view

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/orders"
)

var statusIcon = map[orders.Status]string{
	orders.StatusAwaitingReceipt: "📎", orders.StatusPendingReview: "⏳", orders.StatusApproving: "⏳",
	orders.StatusApproved: "✅", orders.StatusCredited: "💰", orders.StatusFailed: "⚠️",
	orders.StatusRejected: "❌", orders.StatusExpired: "⌛", orders.StatusCancelled: "✖️",
}

var statusText = map[orders.Status]string{
	orders.StatusAwaitingReceipt: "ждёт квитанцию", orders.StatusPendingReview: "на проверке", orders.StatusApproving: "проводится",
	orders.StatusApproved: "оплачен", orders.StatusCredited: "зачислен на баланс", orders.StatusFailed: "ошибка, разбираемся",
	orders.StatusRejected: "отклонён", orders.StatusExpired: "истёк", orders.StatusCancelled: "отменён",
}

func id(prefix string, n int) string { return prefix + strconv.Itoa(n) }

// summary: what is bought and how it is paid.
func summary(o orders.Order) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 <b>Заказ №%s</b>\n%s\n\n", o.Short(), Esc(clip(o.Title, maxTitle)))
	fmt.Fprintf(&b, "Цена: %s\n", Rub(o.Price))
	if o.FromBalance > 0 {
		fmt.Fprintf(&b, "С баланса: −%s\n", Rub(o.FromBalance))
	}
	fmt.Fprintf(&b, "К оплате: <b>%s</b>", Rub(o.ToPay))
	return b.String()
}

func PayScreen(o orders.Order, details string) Screen {
	text := summary(o) + "\n\n💳 <b>Как оплатить</b>\n" + Esc(details) + "\n\n" + TxtReceiptHint
	return Screen{Text: text, Keyboard: kb([]btn{cb(BtnCancel, CbOrderCancel+o.ID)})}
}

// Confirm is the pay screen before the receipt is asked for (bot renew flow).
func Confirm(o orders.Order) Screen {
	if o.Method == orders.MethodBalance {
		return Screen{Text: summary(o) + "\n\nВся сумма спишется с баланса.", Keyboard: kb(
			[]btn{cb(BtnPayBalance, CbOrderBal+o.ID)}, []btn{cb(BtnCancel, CbOrderCancel+o.ID)})}
	}
	return Screen{Text: summary(o), Keyboard: kb(
		[]btn{cb(BtnSendReceipt, CbOrderPay+o.ID)}, []btn{cb(BtnCancel, CbOrderCancel+o.ID)})}
}

type RenewOption struct {
	Months, Days int
	Price        float64
}

func RenewPeriods(subID int, title string, opts []RenewOption) Screen {
	var rows [][]btn
	for _, o := range opts {
		label := fmt.Sprintf("%s — %s", termText(o.Months, o.Days), Rub(o.Price))
		rows = append(rows, []btn{cb(label, fmt.Sprintf("%s%d:%d:%d", CbRenewPick, subID, o.Months, o.Days))})
	}
	rows = append(rows, back(id(CbSub, subID)))
	return Screen{Text: fmt.Sprintf(TxtPickPeriod, Esc(clip(title, maxTitle))), Keyboard: kb(rows...)}
}

func termText(months, days int) string {
	var p []string
	if months > 0 {
		p = append(p, fmt.Sprintf("%d мес.", months))
	}
	if days > 0 {
		p = append(p, fmt.Sprintf("%d дн.", days))
	}
	return strings.Join(p, " ")
}

// AdminCustomer is who sent the receipt, as the admins see them.
type AdminCustomer struct {
	Name       string
	Username   string
	TelegramID int64
}

// AdminCard is the caption of the receipt in the admin chat (≤1024 chars).
func AdminCard(o orders.Order, c AdminCustomer) Screen {
	var b strings.Builder
	b.WriteString(summary(o))
	who := Esc(clip(c.Name, 40))
	if c.Username != "" {
		who += " @" + Esc(c.Username)
	}
	fmt.Fprintf(&b, "\n\n👤 %s · клиент #%d", who, o.CustomerID)
	if o.Comment != "" && o.ReceiptKind != orders.ReceiptText {
		fmt.Fprintf(&b, "\n💬 %s", Esc(clip(o.Comment, 300)))
	}
	if o.Received != nil {
		fmt.Fprintf(&b, "\n✏️ Сумма исправлена %s: %s → %s", Esc(o.Admin), plain(o.ToPay), Rub(*o.Received))
	}
	switch o.Status {
	case orders.StatusPendingReview:
		return Screen{Text: b.String(), Keyboard: adminButtons(o)}
	case orders.StatusApproved:
		fmt.Fprintf(&b, "\n\n✅ Подтвердил %s", Esc(o.Admin))
		if o.ResultUntil != nil {
			fmt.Fprintf(&b, ", действует до %s", Date(o.ResultUntil))
		}
	case orders.StatusCredited:
		fmt.Fprintf(&b, "\n\n💰 %s зачислил на баланс", Esc(o.Admin))
	case orders.StatusRejected:
		fmt.Fprintf(&b, "\n\n❌ Отклонил %s", Esc(o.Admin))
		if o.RejectReason != "" {
			fmt.Fprintf(&b, ": %s", Esc(clip(o.RejectReason, 200)))
		}
	case orders.StatusFailed:
		fmt.Fprintf(&b, "\n\n⚠️ Ошибка: %s", Esc(clip(o.Error, 200)))
		return Screen{Text: b.String(), Keyboard: kb([]btn{cb(BtnRetry, CbAdminRetry+o.ID)})}
	default:
		fmt.Fprintf(&b, "\n\n%s %s", statusIcon[o.Status], statusText[o.Status])
	}
	return Screen{Text: b.String()}
}

// plain is an amount without the currency (for "700 → 500 ₽").
func plain(v float64) string { return strings.TrimSuffix(Rub(v), " ₽") }

func adminButtons(o orders.Order) *models.InlineKeyboardMarkup {
	first := []btn{cb(BtnApprove, CbAdminApprove+o.ID)}
	if o.Paid() < o.ToPay {
		first = nil
		if o.Kind == orders.KindRenew {
			first = append(first, cb(BtnDebt, CbAdminDebt+o.ID))
		}
		first = append(first, cb(BtnCredit, CbAdminCredit+o.ID))
	}
	return kb(first, []btn{cb(BtnEditAmount, CbAdminEdit+o.ID), cb(BtnReject, CbAdminReject+o.ID)})
}

// OrderNotice tells the customer what was decided.
func OrderNotice(o orders.Order) Screen {
	var text string
	switch o.Status {
	case orders.StatusApproved:
		text = fmt.Sprintf("✅ Оплата №%s подтверждена: %s", o.Short(), Esc(o.Title))
		if o.ResultUntil != nil {
			text += fmt.Sprintf(", действует до <b>%s</b>", Date(o.ResultUntil))
		}
	case orders.StatusCredited:
		text = fmt.Sprintf("💰 По заказу №%s получено %s из %s — зачислено на баланс. Доплатите разницу, чтобы завершить покупку.",
			o.Short(), Rub(o.Paid()), Rub(o.ToPay))
	case orders.StatusRejected:
		text = fmt.Sprintf("❌ Платёж №%s отклонён", o.Short())
		if o.RejectReason != "" {
			text += ": " + Esc(clip(o.RejectReason, 200))
		}
	case orders.StatusFailed:
		text = fmt.Sprintf("⚠️ Оплата №%s получена, но применить заказ не удалось. Администратор проверит и свяжется с вами.", o.Short())
	default:
		text = fmt.Sprintf("%s Заказ №%s: %s", statusIcon[o.Status], o.Short(), statusText[o.Status])
	}
	if o.Received != nil && o.Status == orders.StatusApproved && *o.Received != o.ToPay {
		text += fmt.Sprintf("\nПолучено: %s", Rub(*o.Received))
	}
	return Screen{Text: text, Keyboard: kb([]btn{cb(BtnOrders, CbOrders)})}
}

const ordersPerPage = 10

func Orders(list []orders.Order, n, total int) Screen {
	if total == 0 {
		return Screen{Text: "🧾 <b>Платежи</b>\n\nПлатежей пока нет.", Keyboard: kb(back(CbHome))}
	}
	cur, pages, _, _ := page(n, total, ordersPerPage)
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 <b>Платежи</b>%s\n", pageTitle(cur, pages))
	for _, o := range list {
		fmt.Fprintf(&b, "\n%s №%s · %s · %s\n%s — %s", statusIcon[o.Status], o.Short(), Date(&o.CreatedAt), Rub(o.Paid()),
			Esc(clip(o.Title, maxTitle)), statusText[o.Status])
	}
	rows := [][]btn{}
	if nav := pager(CbOrdersPage, cur, pages); nav != nil {
		rows = append(rows, nav)
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows, back(CbHome))...)}
}
