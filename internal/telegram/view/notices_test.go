package view

import (
	"testing"
	"time"

	"vpn-control-bot/internal/notify"
	"vpn-control-bot/internal/vpnctl"
)

func TestNoticeScreens(t *testing.T) {
	gb := int64(1 << 30)
	golden(t, "notice_traffic", TrafficAlert(notify.Notice{
		Kind: notify.Traffic, SubID: 3, Title: "Родители", Used: 41 * gb, Limit: 50 * gb, ResetAt: ptr(now.Add(5 * 24 * time.Hour)),
		Upgrade: &vpnctl.Tariff{Name: "Plus", MonthlyPrice: 200, TrafficLimitBytes: 200 * gb},
	}, links, now))
	golden(t, "notice_traffic_no_reset", TrafficAlert(notify.Notice{
		Kind: notify.Traffic, SubID: 3, Title: "Родители · <lte>", Used: 9 * gb, Limit: 10 * gb,
		Upgrade: &vpnctl.Tariff{Name: "Max", MonthlyPrice: 300},
	}, links, now))
	golden(t, "notice_remind", PaymentReminder(notify.Notice{Kind: notify.Reminder, SubID: 3, Title: "Родители", ExpireAt: now.Add(50 * time.Hour)}, links, now))
	golden(t, "notice_expired", PaymentReminder(notify.Notice{Kind: notify.Reminder, SubID: 3, Title: "Родители", ExpireAt: now.Add(-2 * time.Hour), Expired: true}, links, now))
}
