package view

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/profile"
)

func stateIcon(s profile.State) string {
	switch s {
	case profile.StateActive:
		return "🟢"
	case profile.StateSoon:
		return "🟡"
	case profile.StateExpired:
		return "🔴"
	}
	return "⚪️"
}

func until(loc i18n.Localizer, exp *time.Time, unlimited bool) string {
	if unlimited {
		return "∞"
	}
	return Date(loc, exp)
}

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
		rows = append(rows, []btn{cb(clip(loc.T("btn.renew_sub", s.Title), maxTitle), CbRenew+strconv.Itoa(s.ID))})
	} else if p.NearestExpireAt != nil {
		b.WriteString("\n" + loc.T("home.nearest", Date(loc, p.NearestExpireAt)))
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows,
		[]btn{cb(loc.T("btn.subs"), CbSubs), cb(loc.T("btn.balance"), CbBalance)},
		[]btn{cb(loc.T("btn.refs"), CbRefs), cb(loc.T("btn.tariffs"), CbTariffs)},
		[]btn{cb(loc.T("btn.orders"), CbOrders)},
	)...)}
}

// left is "today" or "in N days".
func left(loc i18n.Localizer, exp, now time.Time) string {
	days := int(exp.Sub(now).Hours() / 24)
	if days < 1 {
		return loc.T("time.today")
	}
	return loc.Plural("time.in_days", days, days)
}

func Subscriptions(loc i18n.Localizer, p profile.Profile, n int, now time.Time) Screen {
	if len(p.Subscriptions) == 0 {
		return Screen{Text: loc.T("subs.empty"),
			Keyboard: kb([]btn{cb(loc.T("btn.tariffs"), CbTariffs)}, back(loc, CbHome))}
	}
	cur, pages, from, to := page(n, len(p.Subscriptions), subsPerPage)
	var b strings.Builder
	b.WriteString(loc.T("subs.title") + pageTitle(loc, cur, pages) + "\n")
	rows := [][]btn{}
	for _, s := range p.Subscriptions[from:to] {
		icon := stateIcon(profile.Expiry(s.ExpireAt, s.Unlimited, now))
		b.WriteString("\n" + loc.T("subs.line", icon, Esc(clip(s.Title, maxTitle)), until(loc, s.ExpireAt, s.Unlimited)))
		if n := len(s.Addons); n > 0 {
			b.WriteString(loc.T("subs.extras", n))
		}
		rows = append(rows, []btn{cb(clip(icon+" "+s.Title, maxTitle), CbSub+strconv.Itoa(s.ID))})
	}
	if nav := pager(loc, CbSubsPage, cur, pages); nav != nil {
		rows = append(rows, nav)
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows, back(loc, CbHome))...)}
}

func Subscription(loc i18n.Localizer, p profile.Profile, id int, l Links, now time.Time) (Screen, bool) {
	for i, s := range p.Subscriptions {
		if s.ID != id {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s <b>%s</b>\n\n", stateIcon(profile.Expiry(s.ExpireAt, s.Unlimited, now)), Esc(clip(s.Title, maxTitle)))
		if s.Tariff != "" {
			b.WriteString(loc.T("sub.tariff", Esc(clip(s.Tariff, maxTitle)), Money(loc, s.Price)) + "\n")
		}
		b.WriteString(loc.T("sub.until", until(loc, s.ExpireAt, s.Unlimited)) + "\n")
		b.WriteString(loc.T("sub.traffic", Bytes(loc, s.UsedBytes), Bytes(loc, s.LimitBytes)) + "\n")
		if s.DeviceLimit != nil {
			b.WriteString(loc.T("sub.devices", *s.DeviceLimit) + "\n")
		}
		if len(s.Addons) > 0 {
			b.WriteString("\n" + loc.T("sub.extras") + "\n")
			for j, a := range s.Addons {
				if j == maxAddons {
					b.WriteString(loc.T("sub.more", len(s.Addons)-maxAddons) + "\n")
					break
				}
				b.WriteString(loc.T("sub.extra_line", stateIcon(profile.Expiry(a.ExpireAt, a.Unlimited, now)), Esc(clip(a.Name, maxTitle)), until(loc, a.ExpireAt, a.Unlimited)))
				if a.Included {
					b.WriteString(loc.T("sub.included"))
				}
				b.WriteString("\n")
			}
		}
		if r := s.Renewal; !s.Unlimited && r.Price > 0 {
			b.WriteString("\n" + loc.T("sub.renew_month", Money(loc, r.Price)) + "\n")
			switch fromBalance := r.FromOwn + r.FromReferral; {
			case r.ToPay == 0:
				b.WriteString(loc.T("sub.renew_all_balance") + "\n")
			case fromBalance > 0:
				b.WriteString(loc.T("sub.renew_split", Money(loc, fromBalance), Money(loc, r.ToPay)) + "\n")
			}
		}
		rows := [][]btn{}
		if !s.Unlimited {
			rows = append(rows, []btn{cb(loc.T("btn.renew"), CbRenew+strconv.Itoa(s.ID))})
		}
		if s.URL != "" {
			fmt.Fprintf(&b, "\n🔗 <code>%s</code>", Esc(s.URL))
			rows = append(rows, []btn{{Text: loc.T("btn.open_link"), WebApp: &models.WebAppInfo{URL: s.URL}}, {Text: loc.T("btn.in_browser"), URL: s.URL}})
		}
		rows = append(rows, []btn{{Text: loc.T("btn.details"), WebApp: l.Page("/subscriptions/" + strconv.Itoa(s.ID))}},
			back(loc, CbSubsPage+strconv.Itoa(i/subsPerPage)))
		return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(rows...)}, true
	}
	return Screen{}, false
}

func Balance(loc i18n.Localizer, p profile.Profile, l Links) Screen {
	var b strings.Builder
	b.WriteString(loc.T("balance.title", Money(loc, p.Balance)) + "\n")
	if p.BalanceReferral != 0 {
		b.WriteString(loc.T("balance.referral", Money(loc, p.BalanceReferral)) + "\n")
	}
	if p.Monthly > 0 {
		b.WriteString(loc.T("balance.monthly", Money(loc, p.Monthly)) + "\n")
	}
	if len(p.Ledger) == 0 {
		b.WriteString("\n" + loc.T("balance.none"))
	} else {
		b.WriteString("\n" + loc.T("balance.recent") + "\n")
		for i, e := range p.Ledger {
			if i == 5 {
				break
			}
			name := e.Type
			if k := "ledger." + e.Type; loc.Has(k) {
				name = loc.T(k)
			}
			d := e.Date
			fmt.Fprintf(&b, "%s · %s · <b>%s</b>\n", Date(loc, &d), name, Money(loc, e.Amount))
		}
	}
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(
		[]btn{{Text: loc.T("btn.history"), WebApp: l.Page("/balance")}},
		back(loc, CbHome),
	)}
}

func Referrals(loc i18n.Localizer, p profile.Profile, l Links) Screen {
	var b strings.Builder
	b.WriteString(loc.T("refs.title") + "\n\n")
	b.WriteString(loc.T("refs.link", Esc(p.RefLink)) + "\n\n")
	if len(p.Referrals) == 0 {
		b.WriteString(loc.T("refs.none"))
	} else {
		b.WriteString(loc.T("refs.stats", len(p.Referrals), Money(loc, p.ReferralsEarned)) + "\n\n" + loc.T("refs.top") + "\n")
		for i, r := range p.Referrals {
			if i == 5 {
				break
			}
			icon := "🟢"
			if !r.Active {
				icon = "⚪️"
			}
			fmt.Fprintf(&b, "%s %s — %s\n", icon, Esc(clip(r.Name, maxTitle)), Money(loc, r.Earned))
		}
	}
	share := "https://t.me/share/url?url=" + url.QueryEscape(p.RefLink) + "&text=" + url.QueryEscape(loc.T("msg.share"))
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(
		[]btn{{Text: loc.T("btn.share"), URL: share}},
		[]btn{{Text: loc.T("btn.copy_link"), CopyText: &models.CopyTextButton{Text: p.RefLink}}},
		[]btn{{Text: loc.T("btn.all_refs"), WebApp: l.Page("/referrals")}},
		back(loc, CbHome),
	)}
}

func Tariffs(loc i18n.Localizer, ts []profile.Tariff, n int, l Links) Screen {
	if len(ts) == 0 {
		return Screen{Text: loc.T("tariffs.empty"), Keyboard: kb(back(loc, CbHome))}
	}
	cur, pages, from, to := page(n, len(ts), tariffsPerPage)
	var b strings.Builder
	b.WriteString(loc.T("tariffs.title") + pageTitle(loc, cur, pages) + "\n")
	var rows [][]btn
	for _, t := range ts[from:to] {
		name := t.Name
		if t.Kind == "addon" && t.AddonName != "" {
			name = t.AddonName + " · " + t.Name
		}
		name = clip(name, maxTitle)
		b.WriteString("\n" + loc.T("tariffs.line", Esc(name), Money(loc, t.MonthlyPrice)))
		if t.Description != "" {
			fmt.Fprintf(&b, "\n%s", Esc(clip(t.Description, maxDescription)))
		}
		var limits []string
		if t.TrafficLimitBytes > 0 {
			limits = append(limits, loc.T("tariffs.traffic", Bytes(loc, t.TrafficLimitBytes)))
		}
		if t.HwidLimit != nil {
			limits = append(limits, loc.Plural("tariffs.devices", *t.HwidLimit, *t.HwidLimit))
		}
		if len(limits) > 0 {
			fmt.Fprintf(&b, "\n<i>%s</i>", strings.Join(limits, " · "))
		}
		b.WriteString("\n")
	}
	if nav := pager(loc, CbTariffsPage, cur, pages); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, []btn{{Text: loc.T("btn.pick_in_app"), WebApp: l.Page("/tariffs")}}, back(loc, CbHome))
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(rows...)}
}

func Notice(loc i18n.Localizer, text string) Screen {
	return Screen{Text: text, Keyboard: kb([]btn{cb(loc.T("btn.home"), CbHome)})}
}

// Plain is a screen with just the catalog text for key.
func Plain(loc i18n.Localizer, key string, args ...any) Screen {
	return Screen{Text: loc.T(key, args...)}
}
