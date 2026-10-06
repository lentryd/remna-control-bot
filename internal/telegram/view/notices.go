package view

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vpn-control-bot/internal/notify"
)

// TrafficAlert: the limit is nearly used up — when it resets and which
// tariff gives more.
func TrafficAlert(n notify.Notice, l Links, now time.Time) Screen {
	var b strings.Builder
	pct := n.Used * 100 / n.Limit
	fmt.Fprintf(&b, "📊 <b>%s</b>: использовано %s из %s (%d%%).\n", Esc(clip(n.Title, maxTitle)), Bytes(n.Used), Bytes(n.Limit), pct)
	if n.ResetAt != nil {
		fmt.Fprintf(&b, "🔄 Лимит сбросится %s — %s.", left(*n.ResetAt, now), Date(n.ResetAt))
	} else {
		b.WriteString("🔄 Лимит обновится после продления.")
	}
	if u := n.Upgrade; u != nil {
		traffic := "безлимитный трафик"
		if u.TrafficLimitBytes > 0 {
			traffic = Bytes(u.TrafficLimitBytes) + " трафика"
		}
		fmt.Fprintf(&b, "\n\n✨ Нужно больше? Тариф <b>%s</b> — %s за %s/мес.", Esc(u.Name), traffic, Rub(u.MonthlyPrice))
	}
	return Screen{Text: b.String(), Keyboard: kb(
		[]btn{{Text: BtnTariffsApp, WebApp: l.Page("/tariffs")}},
		[]btn{cb(BtnOpenSub, CbSub+strconv.Itoa(n.SubID))},
	)}
}

// PaymentReminder: a subscription ends soon or has just ended.
func PaymentReminder(n notify.Notice, l Links, now time.Time) Screen {
	title := Esc(clip(n.Title, maxTitle))
	var text string
	if n.Expired {
		text = fmt.Sprintf("🔴 <b>%s</b> закончилась %s.\nПродлите, чтобы снова пользоваться VPN.", title, Date(&n.ExpireAt))
	} else {
		text = fmt.Sprintf("⏰ <b>%s</b> заканчивается %s — %s.\nПродлите заранее, чтобы VPN не отключился.", title, left(n.ExpireAt, now), Date(&n.ExpireAt))
	}
	return Screen{Text: text, Keyboard: kb(
		[]btn{cb(BtnRenew, CbRenew+strconv.Itoa(n.SubID))},
		[]btn{{Text: BtnDetails, WebApp: l.Page("/subscriptions/" + strconv.Itoa(n.SubID))}},
	)}
}
