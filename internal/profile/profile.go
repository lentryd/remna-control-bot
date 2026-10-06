// Package profile is the customer's own view of their data: what the bot
// screens and the WebApp show. Admin-only fields never get here.
package profile

import (
	"sort"
	"time"

	"vpn-control-bot/internal/vpnctl"
)

type Profile struct {
	ID              int            `json:"id"`
	Name            string         `json:"name"`
	Balance         float64        `json:"balance"`
	BalanceOwn      float64        `json:"balance_own"`
	BalanceReferral float64        `json:"balance_referral"`
	Monthly         float64        `json:"monthly"`
	NearestExpireAt *time.Time     `json:"nearest_expire_at"`
	Subscriptions   []Subscription `json:"subscriptions"`
	Ledger          []LedgerEntry  `json:"ledger"`
	Referrals       []Referral     `json:"referrals"`
	ReferralsEarned float64        `json:"referrals_earned"`
	RefLink         string         `json:"ref_link"`
}

type Subscription struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Tariff      string     `json:"tariff"`
	Price       float64    `json:"price"`
	Status      string     `json:"status"`
	ExpireAt    *time.Time `json:"expire_at"`
	Unlimited   bool       `json:"unlimited"`
	UsedBytes   int64      `json:"used_bytes"`
	LimitBytes  int64      `json:"limit_bytes"`
	DeviceLimit *int       `json:"device_limit"`
	URL         string     `json:"url"`
	Addons      []Addon    `json:"addons"`
}

type Addon struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Tariff     string     `json:"tariff"`
	Price      float64    `json:"price"`
	Included   bool       `json:"included"`
	Status     string     `json:"status"`
	ExpireAt   *time.Time `json:"expire_at"`
	Unlimited  bool       `json:"unlimited"`
	UsedBytes  int64      `json:"used_bytes"`
	LimitBytes int64      `json:"limit_bytes"`
}

type LedgerEntry struct {
	Type   string    `json:"type"`
	Amount float64   `json:"amount"`
	Date   time.Time `json:"date"`
}

type Referral struct {
	Name      string  `json:"name"`
	Active    bool    `json:"active"`
	Monthly   float64 `json:"monthly"`
	TotalPaid float64 `json:"total_paid"`
	Earned    float64 `json:"earned"`
}

func Build(c *vpnctl.Customer, refLink string) Profile {
	p := Profile{
		ID: c.ID, Name: c.Name, Balance: c.Balance, BalanceOwn: c.BalanceOwn, BalanceReferral: c.BalanceReferral,
		Monthly: c.Monthly, NearestExpireAt: c.NearestExpireAt, RefLink: refLink,
		Subscriptions: []Subscription{}, Ledger: []LedgerEntry{}, Referrals: []Referral{},
	}
	for _, s := range c.Subscriptions {
		if s.RwUser != nil && s.RwUser.Deleted {
			continue
		}
		v := Subscription{ID: s.ID, Title: s.Title, Tariff: s.TariffName, Price: s.Price, Status: "PENDING", Addons: []Addon{}}
		if u := s.RwUser; u != nil {
			v.Status, v.ExpireAt, v.Unlimited = u.Status, u.ExpireAt, u.Unlimited
			v.UsedBytes, v.LimitBytes, v.DeviceLimit, v.URL = u.UsedTrafficBytes, u.TrafficLimitBytes, u.HwidDeviceLimit, u.SubscriptionURL
		}
		for _, a := range s.Addons {
			if a.RwUser != nil && a.RwUser.Deleted {
				continue
			}
			av := Addon{ID: a.ID, Name: a.AddonName, Tariff: a.TariffName, Price: a.Price, Included: a.Included, Status: "PENDING"}
			if u := a.RwUser; u != nil {
				av.Status, av.ExpireAt, av.Unlimited = u.Status, u.ExpireAt, u.Unlimited
				av.UsedBytes, av.LimitBytes = u.UsedTrafficBytes, u.TrafficLimitBytes
			}
			v.Addons = append(v.Addons, av)
		}
		p.Subscriptions = append(p.Subscriptions, v)
	}
	for _, l := range c.Ledger {
		p.Ledger = append(p.Ledger, LedgerEntry{Type: l.Type, Amount: l.Amount, Date: l.Date})
	}
	sort.SliceStable(p.Ledger, func(i, j int) bool { return p.Ledger[i].Date.After(p.Ledger[j].Date) })

	earned := map[int]float64{}
	for _, a := range c.Accruals {
		if a.ReferrerID == c.ID {
			earned[a.RefereeID] += a.Amount
			p.ReferralsEarned += a.Amount
		}
	}
	for _, r := range c.Referrals {
		p.Referrals = append(p.Referrals, Referral{Name: r.Name, Active: !r.Archived, Monthly: r.Monthly, TotalPaid: r.TotalPaid, Earned: earned[r.ID]})
	}
	sort.SliceStable(p.Referrals, func(i, j int) bool { return p.Referrals[i].Earned > p.Referrals[j].Earned })
	return p
}

type Tariff struct {
	ID                int             `json:"id"`
	Kind              string          `json:"kind"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	AddonName         string          `json:"addon_name"`
	MonthlyPrice      float64         `json:"monthly_price"`
	TrafficLimitBytes int64           `json:"traffic_limit_bytes"`
	HwidLimit         *int            `json:"hwid_limit"`
	Periods           []vpnctl.Period `json:"periods"`

	order int
}

func Tariffs(in []vpnctl.Tariff) []Tariff {
	out := []Tariff{}
	for _, t := range in {
		if !t.Active {
			continue
		}
		out = append(out, Tariff{
			ID: t.ID, Kind: t.Kind, Name: t.Name, Description: t.Description, AddonName: t.AddonName,
			MonthlyPrice: t.MonthlyPrice, TrafficLimitBytes: t.TrafficLimitBytes, HwidLimit: t.HwidLimit, Periods: t.Periods,
			order: t.SortOrder,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Kind == "base") != (b.Kind == "base") {
			return a.Kind == "base"
		}
		if a.order != b.order {
			return a.order < b.order
		}
		return a.Name < b.Name
	})
	return out
}

type State int

const (
	StateNone State = iota
	StateActive
	StateSoon
	StateExpired
)

func Expiry(exp *time.Time, unlimited bool, now time.Time) State {
	switch {
	case unlimited:
		return StateActive
	case exp == nil:
		return StateNone
	case !exp.After(now):
		return StateExpired
	case exp.Sub(now) < 72*time.Hour:
		return StateSoon
	}
	return StateActive
}
