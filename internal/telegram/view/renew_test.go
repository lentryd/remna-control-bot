package view

import (
	"strings"
	"testing"
	"time"

	"vpn-control-bot/internal/profile"
)

func TestHomeOffersRenewalWhenEndingSoon(t *testing.T) {
	p := fixture() // subscription 3 ends in 48h
	s := Home(ru, p, links, now)
	if !has(webPages(s), renewURL) || !strings.Contains(s.Text, "Основная") {
		t.Fatalf("no renewal offer:\n%s\n%v", s.Text, webPages(s))
	}

	p.Subscriptions[0].ExpireAt = ptr(now.AddDate(0, 2, 0))
	p.NearestExpireAt = p.Subscriptions[0].ExpireAt
	if b := webPages(Home(ru, p, links, now)); has(b, renewURL) {
		t.Fatalf("renewal offered for a subscription two months away: %v", b)
	}
}

func TestHomeRenewalForExpired(t *testing.T) {
	p := fixture()
	p.Subscriptions[0].ExpireAt = ptr(now.Add(-time.Hour))
	s := Home(ru, p, links, now)
	if !has(webPages(s), renewURL) || !strings.Contains(s.Text, "закончилась") {
		t.Fatalf("expired subscription not offered for renewal:\n%s", s.Text)
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
