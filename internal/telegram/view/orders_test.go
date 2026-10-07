package view

import (
	"strings"
	"testing"

	"vpn-control-bot/internal/orders"
)

func order() orders.Order {
	return orders.Order{ID: "0c6f2b3a-5d4e-4f1a-9b7c-8e2d1a3f4b5c", CustomerID: 5, ChatID: 77, Request: orders.Request{Kind: orders.KindRenew, ItemKind: "subscription", ItemID: 3, Months: 3},
		Title: "Продление «Дом» · 3 мес.", Price: 900, FromBalance: 100, ToPay: 800, Method: orders.MethodManual,
		Status: orders.StatusPendingReview, ReceiptKind: orders.ReceiptPhoto, Comment: "перевёл <сбер>", CreatedAt: now}
}

func TestPayScreen(t *testing.T) {
	s := PayScreen(order(), "Карта 2200 <b>")
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
	o := order()
	c := AdminCard(o, AdminCustomer{Name: "Ann", Username: "ann", TelegramID: 77})
	if !strings.Contains(c.Text, "перевёл &lt;сбер&gt;") || !strings.Contains(c.Text, "@ann") || len(c.Text) > 1024 {
		t.Fatalf("card:\n%s", c.Text)
	}
	if b := buttons(c); !has(b, CbAdminApprove+o.ID) || !has(b, CbAdminEdit+o.ID) || !has(b, CbAdminReject+o.ID) {
		t.Fatalf("buttons %v", b)
	}
	r := 500.0
	o.Received = &r
	b := buttons(AdminCard(o, AdminCustomer{Name: "Ann"}))
	if has(b, CbAdminApprove+o.ID) || !has(b, CbAdminDebt+o.ID) || !has(b, CbAdminCredit+o.ID) {
		t.Fatalf("underpaid buttons %v", b)
	}
	o.Kind = orders.KindSubscribe
	if has(buttons(AdminCard(o, AdminCustomer{})), CbAdminDebt+o.ID) {
		t.Fatal("debt offered for a new subscription")
	}
	o.Status, o.Admin = orders.StatusApproved, "@boss"
	done := AdminCard(o, AdminCustomer{})
	if done.Keyboard != nil || !strings.Contains(done.Text, "@boss") || !strings.Contains(done.Text, "800 → 500") {
		t.Fatalf("decided card:\n%s %v", done.Text, done.Keyboard)
	}
	o.Status, o.Error = orders.StatusFailed, "no squad"
	if !has(buttons(AdminCard(o, AdminCustomer{})), CbAdminRetry+o.ID) {
		t.Fatal("no retry on a failed order")
	}
}

func TestOrderNotice(t *testing.T) {
	o := order()
	for st, want := range map[orders.Status]string{
		orders.StatusApproved: "подтверждена", orders.StatusRejected: "отклонён", orders.StatusFailed: "Администратор проверит",
		orders.StatusCredited: "зачислено на баланс",
	} {
		o.Status = st
		r := 500.0
		if st == orders.StatusCredited {
			o.Received = &r
		}
		if s := OrderNotice(o); !strings.Contains(s.Text, want) {
			t.Errorf("%s: %s", st, s.Text)
		}
	}
}

func TestOrdersHistory(t *testing.T) {
	list := []orders.Order{order(), order()}
	list[1].ID, list[1].Status = "b1b1b1b1-0000-4000-8000-000000000000", orders.StatusApproved
	s := Orders(list, 0, 2)
	if !strings.Contains(s.Text, "⏳") || !strings.Contains(s.Text, "✅") || !strings.Contains(s.Text, "№b1b1b1b1") {
		t.Fatal(s.Text)
	}
	if e := Orders(nil, 0, 0); !strings.Contains(e.Text, "пока нет") {
		t.Fatal(e.Text)
	}
}

func TestRenewPeriods(t *testing.T) {
	s := RenewPeriods(3, "Дом", []RenewOption{{Months: 1, Price: 300}, {Months: 3, Price: 800}})
	if b := buttons(s); !has(b, CbRenewPick+"3:1:0") || !has(b, CbRenewPick+"3:3:0") {
		t.Fatalf("%v", b)
	}
}

func TestConfirmWithProviders(t *testing.T) {
	o := order() // manual
	s := Confirm(o, ProviderButton{ID: "yookassa", Title: "Карта / СБП"}, ProviderButton{ID: "stars", Title: "Telegram Stars ⭐"})
	b := buttons(s)
	if !has(b, CbOrderPay+o.ID) || !has(b, CbOrderProvider+"yookassa:"+o.ID) || !has(b, CbOrderProvider+"stars:"+o.ID) || !has(b, CbOrderCancel+o.ID) {
		t.Fatalf("buttons %v", b)
	}
	for _, cb := range b {
		if len(cb) > 64 {
			t.Errorf("callback data %q is over Telegram's 64 bytes", cb)
		}
	}
	undecided := o
	undecided.Method, undecided.Status = "", orders.StatusNew
	if b := buttons(Confirm(undecided, ProviderButton{ID: "stars", Title: "S"})); has(b, CbOrderPay+o.ID) || !has(b, CbOrderProvider+"stars:"+o.ID) {
		t.Errorf("an undecided order offers only providers: %v", b)
	}
	bal := o
	bal.Method = orders.MethodBalance
	if b := buttons(Confirm(bal, ProviderButton{ID: "stars", Title: "S"})); has(b, CbOrderProvider+"stars:"+o.ID) {
		t.Errorf("a balance order offers no provider: %v", b)
	}
}

func TestProviderPay(t *testing.T) {
	o := order()
	o.Status, o.Method, o.PayURL = orders.StatusAwaitingPayment, "yookassa", "https://yoomoney.ru/pay/p1"
	s := ProviderPay(o, "Карта / СБП")
	if !strings.Contains(s.Text, "Карта / СБП") || !strings.Contains(s.Text, "800") {
		t.Fatalf("text:\n%s", s.Text)
	}
	var url string
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			if b.URL != "" {
				url = b.URL
			}
		}
	}
	if url != o.PayURL || !has(buttons(s), CbOrderCancel+o.ID) {
		t.Fatalf("url %q buttons %v", url, buttons(s))
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
	txt := AdminCard(o, AdminCustomer{}).Text
	if !strings.Contains(txt, "700 → 500 $") || strings.Contains(txt, "₽") {
		t.Fatalf("card:\n%s", txt)
	}
}
