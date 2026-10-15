package view

import (
	"testing"
	"time"

	"remna-control-bot/internal/notify"
	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/profile"
	"remna-control-bot/internal/remnactl"
)

// pages maps a button's text to the WebApp URL it opens; any button that is not a web_app fails the test.
func pages(t *testing.T, s Screen) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			if b.WebApp == nil || b.CallbackData != "" {
				t.Fatalf("button %q is not a web_app button: %+v", b.Text, b)
			}
			out[b.Text] = b.WebApp.URL
		}
	}
	return out
}

const renewURL = "https://x.dev/bot/#/checkout?kind=renew&item_kind=subscription&item_id=3"

// webPages lists the WebApp URLs of a screen's buttons.
func webPages(s Screen) []string {
	var out []string
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			if b.WebApp != nil {
				out = append(out, b.WebApp.URL)
			}
		}
	}
	return out
}

func fixtureNotice() notify.Notice {
	return notify.Notice{
		Kind: notify.Traffic, SubID: 3, Title: "Родители", Used: 41 << 30, Limit: 50 << 30, ResetAt: ptr(now.Add(5 * 24 * time.Hour)),
		ExpireAt: now.Add(50 * time.Hour),
		Upgrade:  &remnactl.Tariff{Name: "Plus", MonthlyPrice: 200, TrafficLimitBytes: 200 << 30},
	}
}

func TestHomeOpensAppFirst(t *testing.T) {
	for _, s := range []Screen{Home(ru, fixture(), links, now), Home(ru, fixture(), links, now.AddDate(1, 0, 0))} {
		first := s.Keyboard.InlineKeyboard[0][0]
		if first.WebApp == nil || first.Text != ru.T("btn.app") {
			t.Fatalf("first button = %+v", first)
		}
	}
}

func TestHomeButtonsOpenWebAppPages(t *testing.T) {
	got := pages(t, Home(ru, fixture(), links, now)) // the fixture's subscription ends in 2 days: the renew row is there
	want := map[string]string{
		ru.T("btn.app"):     "https://x.dev/bot/#/",
		ru.T("btn.subs"):    "https://x.dev/bot/#/subscriptions",
		ru.T("btn.balance"): "https://x.dev/bot/#/balance",
		ru.T("btn.refs"):    "https://x.dev/bot/#/referrals",
		ru.T("btn.tariffs"): "https://x.dev/bot/#/tariffs",
		ru.T("btn.orders"):  "https://x.dev/bot/#/orders",
		clip(ru.T("btn.renew_sub", "Основная"), maxTitle): "https://x.dev/bot/#/checkout?kind=renew&item_kind=subscription&item_id=3",
	}
	if len(got) != len(want) {
		t.Fatalf("buttons = %v", got)
	}
	for text, url := range want {
		if got[text] != url {
			t.Errorf("%q opens %q, want %q", text, got[text], url)
		}
	}
}

func TestHomeWithoutSubscriptionsHasNoRenewRow(t *testing.T) {
	got := pages(t, Home(ru, profile.Profile{Name: "Ann"}, links, now))
	if len(got) != 6 {
		t.Fatalf("want the 6 menu buttons, got %v", got)
	}
}

func TestOrderNoticeOpensOrdersPage(t *testing.T) {
	s := OrderNotice(ru, orders.Order{Status: orders.StatusApproved}, links)
	if got := pages(t, s)[ru.T("btn.orders")]; got != "https://x.dev/bot/#/orders" {
		t.Fatalf("opens %q", got)
	}
}

func TestNoticesOpenWebAppPages(t *testing.T) {
	n := fixtureNotice()
	if got := pages(t, TrafficAlert(ru, n, links, now)); got[ru.T("btn.open_sub")] != "https://x.dev/bot/#/subscriptions/3" || got[ru.T("btn.tariffs_app")] != "https://x.dev/bot/#/tariffs" {
		t.Fatalf("traffic: %v", got)
	}
	if got := pages(t, PaymentReminder(ru, n, links, now)); got[ru.T("btn.renew")] != "https://x.dev/bot/#/checkout?kind=renew&item_kind=subscription&item_id=3" || got[ru.T("btn.details")] != "https://x.dev/bot/#/subscriptions/3" {
		t.Fatalf("reminder: %v", got)
	}
}

// buttons returns callback data of every button, in order.
func buttons(s Screen) []string {
	var out []string
	for _, row := range s.Keyboard.InlineKeyboard {
		for _, b := range row {
			out = append(out, b.CallbackData)
		}
	}
	return out
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
