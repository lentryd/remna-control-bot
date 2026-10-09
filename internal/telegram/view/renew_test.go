package view

import (
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/billing"
	"vpn-control-bot/internal/profile"
)

func TestHomeOffersRenewalWhenEndingSoon(t *testing.T) {
	p := fixture() // subscription 3 ends in 48h
	s := Home(ru, p, links, now)
	if !has(buttons(s), CbRenew+"3") || !strings.Contains(s.Text, "Основная") {
		t.Fatalf("no renewal offer:\n%s\n%v", s.Text, buttons(s))
	}

	p.Subscriptions[0].ExpireAt = ptr(now.AddDate(0, 2, 0))
	p.NearestExpireAt = p.Subscriptions[0].ExpireAt
	if b := buttons(Home(ru, p, links, now)); has(b, CbRenew+"3") {
		t.Fatalf("renewal offered for a subscription two months away: %v", b)
	}
}

func TestHomeRenewalForExpired(t *testing.T) {
	p := fixture()
	p.Subscriptions[0].ExpireAt = ptr(now.Add(-time.Hour))
	s := Home(ru, p, links, now)
	if !has(buttons(s), CbRenew+"3") || !strings.Contains(s.Text, "закончилась") {
		t.Fatalf("expired subscription not offered for renewal:\n%s", s.Text)
	}
}

func TestSubscriptionCardHasRenew(t *testing.T) {
	s, _ := Subscription(ru, fixture(), 3, links, now)
	if !has(buttons(s), CbRenew+"3") {
		t.Fatalf("card without renew: %v", buttons(s))
	}
	p := fixture()
	p.Subscriptions[0].Unlimited = true
	if s, _ := Subscription(ru, p, 3, links, now); has(buttons(s), CbRenew+"3") {
		t.Fatal("unlimited subscription offered for renewal")
	}
}

func TestRenewalWindow(t *testing.T) {
	sub := func(d time.Duration, unlimited bool) profile.Subscription {
		return profile.Subscription{ExpireAt: ptr(now.Add(d)), Unlimited: unlimited}
	}
	if !profile.NeedsRenewal(sub(6*24*time.Hour, false), now) || profile.NeedsRenewal(sub(8*24*time.Hour, false), now) ||
		profile.NeedsRenewal(sub(time.Hour, true), now) || profile.NeedsRenewal(profile.Subscription{}, now) {
		t.Fatal("renewal window is not 7 days")
	}
}

func TestSubscriptionCardShowsHowRenewalIsPaid(t *testing.T) {
	p := fixture()
	p.Subscriptions[0].Renewal = billing.Payment{Price: 300, FromReferral: 100, FromOwn: 50, ToPay: 150}
	s, _ := Subscription(ru, p, 3, links, now)
	for _, want := range []string{"Продление на месяц: <b>300 ₽</b>", "с баланса 150 ₽", "к оплате <b>150 ₽</b>"} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("no %q in:\n%s", want, s.Text)
		}
	}
	p.Subscriptions[0].Renewal = billing.Payment{Price: 300, FromOwn: 300}
	if s, _ := Subscription(ru, p, 3, links, now); !strings.Contains(s.Text, "спишется с баланса") {
		t.Errorf("fully covered renewal not explained:\n%s", s.Text)
	}
}
