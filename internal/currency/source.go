// Package currency knows the base currency every amount is in: the one set in the
// remna-control panel, with a configured fallback while the panel can't say.
package currency

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultCode = "RUB"
	defaultTTL  = 10 * time.Minute

	fetchTimeout = 10 * time.Second // one panel call; concurrent Gets wait for it at most this long
)

var codeRe = regexp.MustCompile(`^[A-Z]{3}$`)

// Reader is where the panel's base currency comes from (remnactl.Client).
type Reader interface {
	BaseCurrency(ctx context.Context) (string, error)
}

type Source struct {
	API      Reader
	Fallback string            // used until the panel has answered once; default "RUB"
	TTL      time.Duration     // how long an answer is trusted; default 10 minutes
	Now      func() time.Time  // default time.Now
	Log      *slog.Logger      // may be nil
	OnChange func(code string) // called (outside the lock) when the value first becomes known or changes, including the first Get; may be nil

	mu       sync.Mutex
	cur      string    // last good panel answer; "" until there was one
	known    bool      // the panel has answered successfully at least once; never goes back to false
	next     time.Time // no panel call before this moment; zero: call now
	failing  bool      // inside a failure streak (already logged)
	notified string    // last value passed to OnChange
}

// norm trims and upper-cases code; ok is false when it is not a 3-letter code.
func norm(code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	return code, codeRe.MatchString(code)
}

// Get returns the base currency, refreshing from the panel when the cached answer is older than TTL.
// A failed refresh keeps the last known value (or the fallback if there never was one), logs a warning once per
// failure streak, and does not retry before TTL/10 (at most) has passed so a down panel is not hammered.
func (s *Source) Get(ctx context.Context) string {
	code, changed := s.get(ctx)
	if changed && s.OnChange != nil {
		s.OnChange(code)
	}
	return code
}

func (s *Source) get(ctx context.Context) (code string, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if s.next.IsZero() || !now().Before(s.next) {
		got, err := s.fetch(ctx)
		if err == nil {
			s.cur, s.next, s.failing, s.known = got, now().Add(ttl), false, true
		} else {
			s.next = now().Add(ttl / 10)
			if !s.failing && s.Log != nil {
				s.Log.Warn("currency: panel base currency unavailable", "using", s.value(), "err", err)
			}
			s.failing = true
		}
	}
	code = s.value()
	if code == s.notified {
		return code, false
	}
	s.notified = code
	return code, true
}

// SetFallback changes the currency served while the panel cannot say (the operator's explicit choice).
func (s *Source) SetFallback(code string) {
	s.mu.Lock()
	s.Fallback = code
	s.mu.Unlock()
}

// Known reports whether the panel has answered at least once, i.e. the value served is not just the fallback.
func (s *Source) Known() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.known
}

// value is what Get serves right now; callers hold mu.
func (s *Source) value() string {
	if s.cur != "" {
		return s.cur
	}
	if fb, ok := norm(s.Fallback); ok {
		return fb
	}
	return defaultCode
}

// fetch asks the panel on its own deadline: a caller that gives up (a closed WebApp request) must not turn a good
// refresh into a failure for everybody else.
func (s *Source) fetch(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	raw, err := s.API.BaseCurrency(ctx)
	if err != nil {
		return "", err
	}
	code, ok := norm(raw)
	if !ok {
		return "", fmt.Errorf("panel answered %q, not a currency code", raw)
	}
	return code, nil
}
