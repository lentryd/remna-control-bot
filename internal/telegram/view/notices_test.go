package view

import (
	"testing"
	"time"

	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/notify"
	"remna-control-bot/internal/remnactl"
)

func TestNoticeScreens(t *testing.T) {
	for _, lang := range langs {
		loc := i18n.Default.For(lang)
		gb := int64(1 << 30)
		golden(t, lang, "notice_traffic", TrafficAlert(loc, notify.Notice{
			Kind: notify.Traffic, SubID: 3, Title: "Родители", Used: 41 * gb, Limit: 50 * gb, ResetAt: ptr(now.Add(5 * 24 * time.Hour)),
			Upgrade: &remnactl.Tariff{Name: "Plus", MonthlyPrice: 200, TrafficLimitBytes: 200 * gb},
		}, links, now))
		golden(t, lang, "notice_traffic_no_reset", TrafficAlert(loc, notify.Notice{
			Kind: notify.Traffic, SubID: 3, Title: "Родители · <lte>", Used: 9 * gb, Limit: 10 * gb,
			Upgrade: &remnactl.Tariff{Name: "Max", MonthlyPrice: 300},
		}, links, now))
		golden(t, lang, "notice_remind", PaymentReminder(loc, notify.Notice{Kind: notify.Reminder, SubID: 3, Title: "Родители", ExpireAt: now.Add(50 * time.Hour)}, links, now))
		golden(t, lang, "notice_expired", PaymentReminder(loc, notify.Notice{Kind: notify.Reminder, SubID: 3, Title: "Родители", ExpireAt: now.Add(-2 * time.Hour), Expired: true}, links, now))
	}
}
