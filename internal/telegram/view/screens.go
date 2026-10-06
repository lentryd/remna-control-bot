package view

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

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

func until(exp *time.Time, unlimited bool) string {
	if unlimited {
		return "∞"
	}
	return Date(exp)
}

func Home(p profile.Profile, l Links, now time.Time) Screen {
	var b strings.Builder
	fmt.Fprintf(&b, "👋 <b>%s</b>\n\n", Esc(clip(p.Name, maxTitle)))
	fmt.Fprintf(&b, "💰 Баланс: <b>%s</b>\n", Rub(p.Balance))
	if len(p.Subscriptions) == 0 {
		b.WriteString("📦 Подписок пока нет — загляните в «Тарифы».")
	} else {
		fmt.Fprintf(&b, "📦 Подписок: <b>%d</b>", len(p.Subscriptions))
		if p.Monthly > 0 {
			fmt.Fprintf(&b, " · %s/мес", Rub(p.Monthly))
		}
	}
	rows := [][]btn{{{Text: BtnApp, WebApp: l.Page("/")}}}
	// Subscriptions are sorted by urgency, so the first one is the one to renew.
	if len(p.Subscriptions) > 0 && profile.NeedsRenewal(p.Subscriptions[0], now) {
		s := p.Subscriptions[0]
		title := clip(s.Title, maxTitle)
		if s.ExpireAt.After(now) {
			fmt.Fprintf(&b, "\n\n🟡 «%s» заканчивается %s — %s", Esc(title), left(*s.ExpireAt, now), Date(s.ExpireAt))
		} else {
			fmt.Fprintf(&b, "\n\n🔴 «%s» закончилась %s", Esc(title), Date(s.ExpireAt))
		}
		rows = append(rows, []btn{cb(clip(BtnRenew+" «"+s.Title+"»", maxTitle), CbRenew+strconv.Itoa(s.ID))})
	} else if p.NearestExpireAt != nil {
		fmt.Fprintf(&b, "\n🟢 Ближайшее окончание: <b>%s</b>", Date(p.NearestExpireAt))
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows,
		[]btn{cb(BtnSubs, CbSubs), cb(BtnBalance, CbBalance)},
		[]btn{cb(BtnRefs, CbRefs), cb(BtnTariffs, CbTariffs)},
	)...)}
}

// left is "сегодня" or "через N дн.".
func left(exp, now time.Time) string {
	days := int(exp.Sub(now).Hours() / 24)
	if days < 1 {
		return "сегодня"
	}
	return fmt.Sprintf("через %d дн.", days)
}

func Subscriptions(p profile.Profile, n int, now time.Time) Screen {
	if len(p.Subscriptions) == 0 {
		return Screen{Text: "📦 <b>Подписки</b>\n\nУ вас пока нет подписок. Выберите тариф — и она появится здесь.",
			Keyboard: kb([]btn{cb(BtnTariffs, CbTariffs)}, back(CbHome))}
	}
	cur, pages, from, to := page(n, len(p.Subscriptions), subsPerPage)
	var b strings.Builder
	fmt.Fprintf(&b, "📦 <b>Подписки</b>%s\n", pageTitle(cur, pages))
	rows := [][]btn{}
	for _, s := range p.Subscriptions[from:to] {
		icon := stateIcon(profile.Expiry(s.ExpireAt, s.Unlimited, now))
		fmt.Fprintf(&b, "\n%s <b>%s</b> — до %s", icon, Esc(clip(s.Title, maxTitle)), until(s.ExpireAt, s.Unlimited))
		if n := len(s.Addons); n > 0 {
			fmt.Fprintf(&b, " · аддонов: %d", n)
		}
		rows = append(rows, []btn{cb(clip(icon+" "+s.Title, maxTitle), CbSub+strconv.Itoa(s.ID))})
	}
	if nav := pager(CbSubsPage, cur, pages); nav != nil {
		rows = append(rows, nav)
	}
	return Screen{Text: b.String(), Keyboard: kb(append(rows, back(CbHome))...)}
}

func Subscription(p profile.Profile, id int, l Links, now time.Time) (Screen, bool) {
	for i, s := range p.Subscriptions {
		if s.ID != id {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s <b>%s</b>\n\n", stateIcon(profile.Expiry(s.ExpireAt, s.Unlimited, now)), Esc(clip(s.Title, maxTitle)))
		if s.Tariff != "" {
			fmt.Fprintf(&b, "🏷 Тариф: %s · %s/мес\n", Esc(clip(s.Tariff, maxTitle)), Rub(s.Price))
		}
		fmt.Fprintf(&b, "📅 Действует до: <b>%s</b>\n", until(s.ExpireAt, s.Unlimited))
		fmt.Fprintf(&b, "📊 Трафик: %s из %s\n", Bytes(s.UsedBytes), Bytes(s.LimitBytes))
		if s.DeviceLimit != nil {
			fmt.Fprintf(&b, "📱 Устройств: до %d\n", *s.DeviceLimit)
		}
		if len(s.Addons) > 0 {
			b.WriteString("\n<b>Аддоны</b>\n")
			for j, a := range s.Addons {
				if j == maxAddons {
					fmt.Fprintf(&b, "…и ещё %d — в приложении\n", len(s.Addons)-maxAddons)
					break
				}
				fmt.Fprintf(&b, "%s %s — до %s", stateIcon(profile.Expiry(a.ExpireAt, a.Unlimited, now)), Esc(clip(a.Name, maxTitle)), until(a.ExpireAt, a.Unlimited))
				if a.Included {
					b.WriteString(" (входит в тариф)")
				}
				b.WriteString("\n")
			}
		}
		if r := s.Renewal; !s.Unlimited && r.Price > 0 {
			fmt.Fprintf(&b, "\n💳 Продление на месяц: <b>%s</b>\n", Rub(r.Price))
			switch fromBalance := r.FromOwn + r.FromReferral; {
			case r.ToPay == 0:
				b.WriteString("Вся сумма спишется с баланса\n")
			case fromBalance > 0:
				fmt.Fprintf(&b, "с баланса %s · к оплате <b>%s</b>\n", Rub(fromBalance), Rub(r.ToPay))
			}
		}
		rows := [][]btn{}
		if !s.Unlimited {
			rows = append(rows, []btn{cb(BtnRenew, CbRenew+strconv.Itoa(s.ID))})
		}
		if s.URL != "" {
			fmt.Fprintf(&b, "\n🔗 <code>%s</code>", Esc(s.URL))
			rows = append(rows, []btn{{Text: BtnOpenLink, WebApp: &models.WebAppInfo{URL: s.URL}}, {Text: BtnInBrowser, URL: s.URL}})
		}
		rows = append(rows, []btn{{Text: BtnDetails, WebApp: l.Page("/subscriptions/" + strconv.Itoa(s.ID))}},
			back(CbSubsPage+strconv.Itoa(i/subsPerPage)))
		return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(rows...)}, true
	}
	return Screen{}, false
}

var ledgerTypes = map[string]string{
	"payment": "Пополнение", "charge": "Списание", "adjustment": "Корректировка", "refund": "Возврат", "referral": "Реферальное начисление",
}

func Balance(p profile.Profile, l Links) Screen {
	var b strings.Builder
	fmt.Fprintf(&b, "💰 <b>Баланс: %s</b>\n", Rub(p.Balance))
	if p.BalanceReferral != 0 {
		fmt.Fprintf(&b, "из них реферальных: %s\n", Rub(p.BalanceReferral))
	}
	if p.Monthly > 0 {
		fmt.Fprintf(&b, "Ежемесячно: %s\n", Rub(p.Monthly))
	}
	if len(p.Ledger) == 0 {
		b.WriteString("\nОпераций пока не было.")
	} else {
		b.WriteString("\n<b>Последние операции</b>\n")
		for i, e := range p.Ledger {
			if i == 5 {
				break
			}
			name := ledgerTypes[e.Type]
			if name == "" {
				name = e.Type
			}
			d := e.Date
			fmt.Fprintf(&b, "%s · %s · <b>%s</b>\n", Date(&d), name, Rub(e.Amount))
		}
	}
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(
		[]btn{{Text: BtnHistory, WebApp: l.Page("/balance")}},
		back(CbHome),
	)}
}

func Referrals(p profile.Profile, l Links) Screen {
	var b strings.Builder
	b.WriteString("👥 <b>Рефералы</b>\n\n")
	fmt.Fprintf(&b, "Ваша ссылка:\n<code>%s</code>\n\n", Esc(p.RefLink))
	if len(p.Referrals) == 0 {
		b.WriteString("Пока никого. Поделитесь ссылкой — за оплаты приглашённых вы получаете процент на баланс.")
	} else {
		fmt.Fprintf(&b, "Приглашено: <b>%d</b> · заработано: <b>%s</b>\n\n<b>Топ по доходу</b>\n", len(p.Referrals), Rub(p.ReferralsEarned))
		for i, r := range p.Referrals {
			if i == 5 {
				break
			}
			icon := "🟢"
			if !r.Active {
				icon = "⚪️"
			}
			fmt.Fprintf(&b, "%s %s — %s\n", icon, Esc(clip(r.Name, maxTitle)), Rub(r.Earned))
		}
	}
	share := "https://t.me/share/url?url=" + url.QueryEscape(p.RefLink) + "&text=" + url.QueryEscape(TxtShare)
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(
		[]btn{{Text: BtnShare, URL: share}},
		[]btn{{Text: BtnCopyLink, CopyText: &models.CopyTextButton{Text: p.RefLink}}},
		[]btn{{Text: BtnAllRefs, WebApp: l.Page("/referrals")}},
		back(CbHome),
	)}
}

func Tariffs(ts []profile.Tariff, n int, l Links) Screen {
	if len(ts) == 0 {
		return Screen{Text: "🏷 <b>Тарифы</b>\n\nСейчас нет доступных тарифов.", Keyboard: kb(back(CbHome))}
	}
	cur, pages, from, to := page(n, len(ts), tariffsPerPage)
	var b strings.Builder
	fmt.Fprintf(&b, "🏷 <b>Тарифы</b>%s\n", pageTitle(cur, pages))
	var rows [][]btn
	for _, t := range ts[from:to] {
		name := t.Name
		if t.Kind == "addon" && t.AddonName != "" {
			name = t.AddonName + " · " + t.Name
		}
		name = clip(name, maxTitle)
		fmt.Fprintf(&b, "\n<b>%s</b> — %s/мес", Esc(name), Rub(t.MonthlyPrice))
		if t.Description != "" {
			fmt.Fprintf(&b, "\n%s", Esc(clip(t.Description, maxDescription)))
		}
		var limits []string
		if t.TrafficLimitBytes > 0 {
			limits = append(limits, "трафик "+Bytes(t.TrafficLimitBytes))
		}
		if t.HwidLimit != nil {
			limits = append(limits, fmt.Sprintf("до %d устройств", *t.HwidLimit))
		}
		if len(limits) > 0 {
			fmt.Fprintf(&b, "\n<i>%s</i>", strings.Join(limits, " · "))
		}
		b.WriteString("\n")
	}
	if nav := pager(CbTariffsPage, cur, pages); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, []btn{{Text: BtnPickInApp, WebApp: l.Page("/tariffs")}}, back(CbHome))
	return Screen{Text: strings.TrimRight(b.String(), "\n"), Keyboard: kb(rows...)}
}

func Notice(text string) Screen { return Screen{Text: text, Keyboard: kb([]btn{cb(BtnHome, CbHome)})} }

func Plain(text string) Screen { return Screen{Text: text} }
