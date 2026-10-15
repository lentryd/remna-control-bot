package view

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/profile"
)

func Home(loc i18n.Localizer, p profile.Profile, l Links, now time.Time) Screen {
	var b strings.Builder
	fmt.Fprintf(&b, "👋 <b>%s</b>\n\n", Esc(clip(p.Name, maxTitle)))
	b.WriteString(loc.T("home.balance", Money(loc, p.Balance)) + "\n")
	if len(p.Subscriptions) == 0 {
		b.WriteString(loc.T("home.no_subs"))
	} else {
		b.WriteString(loc.T("home.subs_count", len(p.Subscriptions)))
		if p.Monthly > 0 {
			b.WriteString(loc.T("home.per_month", Money(loc, p.Monthly)))
		}
	}
	rows := [][]btn{{{Text: loc.T("btn.app"), WebApp: l.Page("/")}}}
	// Subscriptions are sorted by urgency, so the first one is the one to renew.
	if len(p.Subscriptions) > 0 && profile.NeedsRenewal(p.Subscriptions[0], now) {
		s := p.Subscriptions[0]
		title := clip(s.Title, maxTitle)
		if s.ExpireAt.After(now) {
			b.WriteString("\n\n" + loc.T("home.soon", Esc(title), left(loc, *s.ExpireAt, now), Date(loc, s.ExpireAt)))
		} else {
			b.WriteString("\n\n" + loc.T("home.expired", Esc(title), Date(loc, s.ExpireAt)))
		}
		rows = append(rows, []btn{{Text: clip(loc.T("btn.renew_sub", s.Title), maxTitle), WebApp: l.Page(renewPage(s.ID))}})
	} else if p.NearestExpireAt != nil {
		b.WriteString("\n" + loc.T("home.nearest", Date(loc, p.NearestExpireAt)))
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows,
		[]btn{{Text: loc.T("btn.subs"), WebApp: l.Page("/subscriptions")}, {Text: loc.T("btn.balance"), WebApp: l.Page("/balance")}},
		[]btn{{Text: loc.T("btn.refs"), WebApp: l.Page("/referrals")}, {Text: loc.T("btn.tariffs"), WebApp: l.Page("/tariffs")}},
		[]btn{{Text: loc.T("btn.orders"), WebApp: l.Page("/orders")}},
	)...)}
}

// renewPage is the WebApp checkout that renews a subscription (web/src/renew.ts checkoutRenew).
func renewPage(subID int) string {
	return "/checkout?kind=renew&item_kind=subscription&item_id=" + strconv.Itoa(subID)
}

// left is "today" or "in N days".
func left(loc i18n.Localizer, exp, now time.Time) string {
	days := int(exp.Sub(now).Hours() / 24)
	if days < 1 {
		return loc.T("time.today")
	}
	return loc.Plural("time.in_days", days, days)
}

func Notice(loc i18n.Localizer, text string) Screen {
	return Screen{Text: text, Keyboard: kb([]btn{cb(loc.T("btn.home"), CbHome)})}
}

// Plain is a screen with just the catalog text for key.
func Plain(loc i18n.Localizer, key string, args ...any) Screen {
	return Screen{Text: loc.T(key, args...)}
}
