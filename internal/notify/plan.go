// Package notify decides which Telegram notifications to send (traffic
// running out, payment reminders) and sends each of them once.
package notify

import (
	"fmt"
	"strconv"
	"time"

	"remna-control-bot/internal/remnactl"
)

type Kind int

const (
	Traffic  Kind = iota // traffic used ≥ TrafficPct of the limit
	Reminder             // subscription/add-on ends soon or just ended
)

// Notice is one notification for one chat. Key identifies it for
// deduplication: the same Key is never sent twice.
type Notice struct {
	Key    string
	Kind   Kind
	ChatID int64
	SubID  int    // subscription to renew / open
	Title  string // "Sub" or "Sub · Add-on"

	// Traffic
	Used, Limit int64
	ResetAt     *time.Time       // nil: the limit isn't reset until renewal
	Upgrade     *remnactl.Tariff // cheapest paid tariff with more traffic, if any

	// Reminder
	ExpireAt time.Time
	Expired  bool
}

type Rules struct {
	TrafficPct int
	RemindDays []int // descending, e.g. 3,1
}

// expiredGrace: reminders about an ended subscription are sent only this
// soon after the end, so an old expired one doesn't get a notice on start.
const expiredGrace = 3 * 24 * time.Hour

// item is a subscription or one of its add-ons, whatever has a panel user.
type item struct {
	kind    string // "sub" | "addon"
	id      int
	subID   int
	title   string
	tariff  *int
	addonID int
	u       *remnactl.RwUser
}

func Plan(customers []remnactl.Customer, subs []remnactl.Subscription, tariffs []remnactl.Tariff, r Rules, now time.Time) []Notice {
	chats := map[int]int64{}
	for _, c := range customers {
		if c.TelegramID != nil && !c.Archived {
			chats[c.ID] = *c.TelegramID
		}
	}
	var out []Notice
	for _, s := range subs {
		chat, ok := chats[s.CustomerID]
		if !ok {
			continue
		}
		items := []item{{kind: "sub", id: s.ID, subID: s.ID, title: s.Title, tariff: s.TariffID, u: s.RwUser}}
		for _, a := range s.Addons {
			items = append(items, item{kind: "addon", id: a.ID, subID: s.ID, title: s.Title + " · " + a.AddonName, tariff: a.TariffID, addonID: a.AddonID, u: a.RwUser})
		}
		for _, it := range items {
			if it.u == nil || it.u.Deleted || it.u.Status == "DISABLED" {
				continue
			}
			if n, ok := traffic(it, tariffs, r, now); ok {
				n.ChatID = chat
				out = append(out, n)
			}
			if n, ok := reminder(it, r, now); ok && (it.kind != "addon" || !sameDay(it.u.ExpireAt, s.RwUser)) {
				n.ChatID = chat
				out = append(out, n)
			}
		}
	}
	return out
}

func traffic(it item, tariffs []remnactl.Tariff, r Rules, now time.Time) (Notice, bool) {
	u := it.u
	if u.TrafficLimitBytes <= 0 || u.UsedTrafficBytes*100 < u.TrafficLimitBytes*int64(r.TrafficPct) {
		return Notice{}, false
	}
	n := Notice{Kind: Traffic, SubID: it.subID, Title: it.title, Used: u.UsedTrafficBytes, Limit: u.TrafficLimitBytes}
	// one alert per traffic period: until the reset, or until renewal
	period := "none"
	if u.TrafficLimitStrategy != "NO_RESET" && u.NextTrafficResetAt != nil {
		n.ResetAt = u.NextTrafficResetAt
		period = u.NextTrafficResetAt.Format("2006-01-02")
	} else if u.ExpireAt != nil {
		period = "until-" + u.ExpireAt.Format("2006-01-02")
	}
	n.Key = fmt.Sprintf("traffic:%s:%d:%s", it.kind, it.id, period)
	n.Upgrade = upgrade(it, tariffs)
	return n, true
}

// upgrade is the cheapest active paid tariff of the same kind (the same
// add-on for add-ons) that gives more traffic than the current limit.
func upgrade(it item, tariffs []remnactl.Tariff) *remnactl.Tariff {
	var best *remnactl.Tariff
	for i := range tariffs {
		t := &tariffs[i]
		if !t.Active || t.MonthlyPrice <= 0 || (it.tariff != nil && t.ID == *it.tariff) {
			continue
		}
		if it.kind == "sub" && t.Kind != "base" {
			continue
		}
		if it.kind == "addon" && (t.Kind != "addon" || t.AddonID == nil || *t.AddonID != it.addonID) {
			continue
		}
		if t.TrafficLimitBytes != 0 && t.TrafficLimitBytes <= it.u.TrafficLimitBytes {
			continue
		}
		if best == nil || t.MonthlyPrice < best.MonthlyPrice {
			best = t
		}
	}
	return best
}

func reminder(it item, r Rules, now time.Time) (Notice, bool) {
	u := it.u
	if u.Unlimited || u.ExpireAt == nil {
		return Notice{}, false
	}
	left := u.ExpireAt.Sub(now)
	stage := -1
	switch {
	case left <= 0 && left > -expiredGrace:
		stage = 0
	case left > 0:
		for _, d := range r.RemindDays { // descending: keep the smallest that fits
			if left <= time.Duration(d)*24*time.Hour {
				stage = d
			}
		}
	}
	if stage < 0 {
		return Notice{}, false
	}
	return Notice{
		Kind: Reminder, SubID: it.subID, Title: it.title, ExpireAt: *u.ExpireAt, Expired: left <= 0,
		Key: fmt.Sprintf("remind:%s:%d:%s:%s", it.kind, it.id, u.ExpireAt.Format("2006-01-02"), strconv.Itoa(stage)),
	}, true
}

// sameDay: an add-on ending the same day as its subscription is renewed
// with it, so it gets no reminder of its own.
func sameDay(addonEnd *time.Time, sub *remnactl.RwUser) bool {
	if addonEnd == nil || sub == nil || sub.ExpireAt == nil {
		return false
	}
	return addonEnd.Format("2006-01-02") == sub.ExpireAt.Format("2006-01-02")
}
