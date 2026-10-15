package view

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/orders"
)

func order() orders.Order {
	return orders.Order{ID: "0c6f2b3a-5d4e-4f1a-9b7c-8e2d1a3f4b5c", CustomerID: 5, ChatID: 77, Request: orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3},
		Title: "Продление «Дом» · 3 мес.", Price: 900, FromBalance: 100, ToPay: 800, Method: orders.MethodManual,
		Status: orders.StatusPendingReview, ReceiptKind: orders.ReceiptPhoto, Comment: "перевёл <сбер>", CreatedAt: now}
}

func TestPayScreen(t *testing.T) {
	s := PayScreen(ru, order(), "Карта 2200 <b>")
	for _, want := range []string{"Заказ №0c6f2b3a", "900", "100", "<b>800 ₽</b>", "Карта 2200 &lt;b&gt;"} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("no %q in\n%s", want, s.Text)
		}
	}
	if b := buttons(s); !has(b, CbOrderCancel+order().ID) {
		t.Errorf("buttons %v", b)
	}
}

func TestAdminCard(t *testing.T) {
	open := &models.WebAppInfo{URL: "https://x/bot/#/admin/orders/1"}
	o := orders.Order{ID: "12345678-aaaa", CustomerID: 5, Title: "Продление", Price: 700, ToPay: 700, Status: orders.StatusPendingReview,
		ReceiptKind: orders.ReceiptPhoto, Comment: "перевёл", Method: orders.MethodManual}
	s := AdminCard(ru, o, AdminCustomer{Name: "Ann", Username: "ann"}, open)
	for _, want := range []string{"№12345678", "Ann @ann", "клиент #5", "перевёл", "чек об оплате — в приложении", "Ждёт решения", "Способ: перевод с чеком об оплате"} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("card lacks %q:\n%s", want, s.Text)
		}
	}
	if len(s.Keyboard.InlineKeyboard) != 1 || s.Keyboard.InlineKeyboard[0][0].WebApp != open || s.Keyboard.InlineKeyboard[0][0].CallbackData != "" {
		t.Fatalf("the only button must open the web app: %+v", s.Keyboard)
	}
	for m, want := range map[orders.Method]string{orders.MethodBalance: "Способ: баланс", "cryptobot": "Способ: CryptoBot", "newpay": "Способ: newpay"} {
		o.Method = m
		if s = AdminCard(ru, o, AdminCustomer{}, open); !strings.Contains(s.Text, want) {
			t.Errorf("method %q: card lacks %q:\n%s", m, want, s.Text)
		}
	}
	o.Status, o.Admin = orders.StatusApproved, "@boss"
	if s = AdminCard(ru, o, AdminCustomer{}, open); !strings.Contains(s.Text, "Подтвердил @boss") || len(s.Keyboard.InlineKeyboard) != 1 {
		t.Fatalf("resolved card keeps the open button: %+v", s)
	}
	if s = AdminCard(ru, order(), AdminCustomer{Name: "<Ann>"}, open); !strings.Contains(s.Text, "перевёл &lt;сбер&gt;") || !strings.Contains(s.Text, "&lt;Ann&gt;") {
		t.Fatalf("customer text must be escaped:\n%s", s.Text)
	}
}

func TestOrderNotice(t *testing.T) {
	o := order()
	for st, want := range map[orders.Status]string{
		orders.StatusApproved: "подтверждена", orders.StatusRejected: "отклонён", orders.StatusFailed: "Поддержка проверит",
		orders.StatusCredited: "зачислено на баланс",
	} {
		o.Status = st
		r := 500.0
		if st == orders.StatusCredited {
			o.Received = &r
		}
		if s := OrderNotice(ru, o, links); !strings.Contains(s.Text, want) {
			t.Errorf("%s: %s", st, s.Text)
		}
	}
}

func TestAdminCardCurrency(t *testing.T) {
	t.Cleanup(func() { SetCurrency("RUB") })
	SetCurrency("USD")
	o := order()
	o.ToPay = 700
	r := 500.0
	o.Received = &r
	o.Status, o.Admin = orders.StatusApproved, "@boss"
	txt := AdminCard(ru, o, AdminCustomer{}, nil).Text
	if !strings.Contains(txt, "700 → 500 $") || strings.Contains(txt, "₽") {
		t.Fatalf("card:\n%s", txt)
	}
}

// A title stored as a catalog message is read in the viewer's language; a legacy title is shown as stored.
func TestStoredOrderTextsPerViewer(t *testing.T) {
	en := i18n.Default.For("en")
	o := order()
	o.Title = i18n.M("title.renew", "Дом <b>", i18n.M("term.months", 3)).String()
	o.Status, o.Error = orders.StatusFailed, i18n.M("alert.result_unknown", "bot order #1", "bad gateway").String()
	o.Comment = i18n.M("review.wrong_amount", "pal24", "800 RUB").String()
	for loc, want := range map[i18n.Localizer][]string{
		ru: {"Продление «Дом &lt;b&gt;» · 3 мес.", "VPN-панели", "неверной сумме"},
		en: {"Renewal of “Дом &lt;b&gt;” · 3 mo.", "VPN panel", "wrong amount"},
	} {
		card := AdminCard(loc, o, AdminCustomer{}, nil).Text
		list := PayScreen(loc, o, "details").Text
		for _, w := range want {
			if !strings.Contains(card, w) {
				t.Errorf("%s card lacks %q:\n%s", loc.Lang(), w, card)
			}
		}
		if !strings.Contains(list, want[0]) || strings.Contains(card+list, "i18n:") {
			t.Errorf("%s list:\n%s", loc.Lang(), list)
		}
	}
	o.Status, o.Title = orders.StatusApproved, "Продление «Дом» · 3 мес." // a row written before titles were localized
	if s := OrderNotice(en, o, links).Text; !strings.Contains(s, "Продление «Дом» · 3 мес.") {
		t.Errorf("legacy title must be shown as stored: %s", s)
	}
}
