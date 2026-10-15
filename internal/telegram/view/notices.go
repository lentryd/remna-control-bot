package view

import (
	"strconv"
	"strings"
	"time"

	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/notify"
)

// TrafficAlert: the limit is nearly used up — when it resets and which
// tariff gives more.
func TrafficAlert(loc i18n.Localizer, n notify.Notice, l Links, now time.Time) Screen {
	var b strings.Builder
	pct := n.Used * 100 / n.Limit
	b.WriteString(loc.T("notice.traffic", Esc(clip(n.Title, maxTitle)), Bytes(loc, n.Used), Bytes(loc, n.Limit), pct) + "\n")
	if n.ResetAt != nil {
		b.WriteString(loc.T("notice.reset", left(loc, *n.ResetAt, now), Date(loc, n.ResetAt)))
	} else {
		b.WriteString(loc.T("notice.reset_renew"))
	}
	if u := n.Upgrade; u != nil {
		traffic := loc.T("notice.unlimited")
		if u.TrafficLimitBytes > 0 {
			traffic = loc.T("notice.traffic_amount", Bytes(loc, u.TrafficLimitBytes))
		}
		b.WriteString("\n\n" + loc.T("notice.upgrade", Esc(u.Name), traffic, Money(loc, u.MonthlyPrice)))
	}
	return Screen{Text: b.String(), Keyboard: kb(
		[]btn{{Text: loc.T("btn.tariffs_app"), WebApp: l.Page("/tariffs")}},
		[]btn{{Text: loc.T("btn.open_sub"), WebApp: l.Page("/subscriptions/" + strconv.Itoa(n.SubID))}},
	)}
}

// PaymentReminder: a subscription ends soon or has just ended.
func PaymentReminder(loc i18n.Localizer, n notify.Notice, l Links, now time.Time) Screen {
	title := Esc(clip(n.Title, maxTitle))
	var text string
	if n.Expired {
		text = loc.T("notice.expired", title, Date(loc, &n.ExpireAt))
	} else {
		text = loc.T("notice.remind", title, left(loc, n.ExpireAt, now), Date(loc, &n.ExpireAt))
	}
	return Screen{Text: text, Keyboard: kb(
		[]btn{{Text: loc.T("btn.renew"), WebApp: l.Page(renewPage(n.SubID))}},
		[]btn{{Text: loc.T("btn.details"), WebApp: l.Page("/subscriptions/" + strconv.Itoa(n.SubID))}},
	)}
}
