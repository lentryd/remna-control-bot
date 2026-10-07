package currency

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeReader struct {
	mu    sync.Mutex
	code  string
	err   error
	calls int
}

func (f *fakeReader) BaseCurrency(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.code, f.err
}

func (f *fakeReader) set(code string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.code, f.err = code, err
}

func (f *fakeReader) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type rig struct {
	src     *Source
	api     *fakeReader
	clk     *clock
	changes []string
	logs    *bytes.Buffer
}

func newRig(fallback string) *rig {
	r := &rig{api: &fakeReader{code: "USD"}, clk: &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{}}
	r.src = &Source{
		API: r.api, Fallback: fallback, TTL: 10 * time.Minute, Now: r.clk.now,
		Log:      slog.New(slog.NewTextHandler(r.logs, nil)),
		OnChange: func(code string) { r.changes = append(r.changes, code) },
	}
	return r
}

func TestFirstGetHitsPanelAndCaches(t *testing.T) {
	r := newRig("RUB")
	ctx := context.Background()
	if got := r.src.Get(ctx); got != "USD" {
		t.Fatalf("got %q", got)
	}
	if got := r.src.Get(ctx); got != "USD" || r.api.count() != 1 {
		t.Fatalf("got %q after %d calls", got, r.api.count())
	}
	if len(r.changes) != 1 || r.changes[0] != "USD" {
		t.Fatalf("changes %v", r.changes)
	}
}

func TestNoRefetchBeforeTTLRefetchAfter(t *testing.T) {
	r := newRig("RUB")
	ctx := context.Background()
	r.src.Get(ctx)
	r.api.set("EUR", nil)
	r.clk.advance(9 * time.Minute)
	if got := r.src.Get(ctx); got != "USD" || r.api.count() != 1 {
		t.Fatalf("before TTL: %q, %d calls", got, r.api.count())
	}
	r.clk.advance(2 * time.Minute)
	if got := r.src.Get(ctx); got != "EUR" || r.api.count() != 2 {
		t.Fatalf("after TTL: %q, %d calls", got, r.api.count())
	}
	if want := []string{"USD", "EUR"}; strings.Join(r.changes, ",") != strings.Join(want, ",") {
		t.Fatalf("changes %v", r.changes)
	}
	// Unchanged refetch does not fire OnChange again.
	r.clk.advance(11 * time.Minute)
	r.src.Get(ctx)
	if len(r.changes) != 2 || r.api.count() != 3 {
		t.Fatalf("changes %v, calls %d", r.changes, r.api.count())
	}
}

func TestErrorBeforeSuccessFallsBack(t *testing.T) {
	r := newRig("eur ")
	r.api.set("", errors.New("down"))
	ctx := context.Background()
	if got := r.src.Get(ctx); got != "EUR" {
		t.Fatalf("got %q", got)
	}
	if len(r.changes) != 1 || r.changes[0] != "EUR" {
		t.Fatalf("changes %v", r.changes)
	}
	// Retry only after TTL/10.
	r.clk.advance(30 * time.Second)
	r.src.Get(ctx)
	if r.api.count() != 1 {
		t.Fatalf("retried too early: %d calls", r.api.count())
	}
	r.clk.advance(31 * time.Second)
	r.src.Get(ctx)
	if r.api.count() != 2 || len(r.changes) != 1 {
		t.Fatalf("calls %d, changes %v", r.api.count(), r.changes)
	}
	if n := strings.Count(r.logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("warnings = %d: %s", n, r.logs.String())
	}
	// The panel answers with the fallback itself: still no second OnChange.
	r.api.set("EUR", nil)
	r.clk.advance(time.Minute + time.Second)
	if got := r.src.Get(ctx); got != "EUR" || len(r.changes) != 1 {
		t.Fatalf("got %q, changes %v", got, r.changes)
	}
}

func TestErrorAfterSuccessKeepsValueLogsOncePerStreak(t *testing.T) {
	r := newRig("RUB")
	ctx := context.Background()
	r.src.Get(ctx)
	r.api.set("", errors.New("down"))
	for i := 0; i < 4; i++ {
		r.clk.advance(11 * time.Minute)
		if got := r.src.Get(ctx); got != "USD" {
			t.Fatalf("got %q", got)
		}
	}
	if n := strings.Count(r.logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("warnings = %d: %s", n, r.logs.String())
	}
	if len(r.changes) != 1 {
		t.Fatalf("changes %v", r.changes)
	}
	// Recovery with a new value, then a second failure streak warns again.
	r.api.set("EUR", nil)
	r.clk.advance(11 * time.Minute)
	if got := r.src.Get(ctx); got != "EUR" {
		t.Fatalf("recovered: %q", got)
	}
	if want := "USD,EUR"; strings.Join(r.changes, ",") != want {
		t.Fatalf("changes %v", r.changes)
	}
	r.api.set("", errors.New("down again"))
	r.clk.advance(11 * time.Minute)
	if got := r.src.Get(ctx); got != "EUR" {
		t.Fatalf("got %q", got)
	}
	if n := strings.Count(r.logs.String(), "level=WARN"); n != 2 {
		t.Fatalf("warnings = %d: %s", n, r.logs.String())
	}
}

func TestInvalidPanelAnswerIsAFailure(t *testing.T) {
	r := newRig("RUB")
	r.api.set("US", nil)
	if got := r.src.Get(context.Background()); got != "RUB" {
		t.Fatalf("got %q", got)
	}
}

func TestFallbackNormalisation(t *testing.T) {
	for in, want := range map[string]string{"": "RUB", "usd": "USD", " eur ": "EUR", "US": "RUB", "USDT": "RUB", "1": "RUB"} {
		r := newRig(in)
		r.api.set("", errors.New("down"))
		if got := r.src.Get(context.Background()); got != want {
			t.Errorf("fallback %q: got %q, want %q", in, got, want)
		}
	}
}

func TestDefaultsAndNilCallbacks(t *testing.T) {
	api := &fakeReader{code: "usd"}
	s := &Source{API: api}
	if got := s.Get(context.Background()); got != "USD" {
		t.Fatalf("got %q", got)
	}
	s.Get(context.Background())
	if api.count() != 1 {
		t.Fatalf("calls %d", api.count())
	}
}

func TestConcurrentGets(t *testing.T) {
	r := newRig("RUB")
	var changes atomic.Int32
	r.src.OnChange = func(string) { changes.Add(1) }
	run := func() {
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.src.Get(context.Background())
			}()
		}
		wg.Wait()
	}
	run()
	if r.api.count() != 1 {
		t.Fatalf("calls %d", r.api.count())
	}
	r.api.set("EUR", nil)
	r.clk.advance(11 * time.Minute)
	run()
	if r.api.count() != 2 || changes.Load() != 2 {
		t.Fatalf("calls %d, changes %d", r.api.count(), changes.Load())
	}
}

func TestKnownOnlyAfterAPanelAnswer(t *testing.T) {
	r := newRig("RUB")
	ctx := context.Background()
	if r.src.Known() {
		t.Fatal("known before any call")
	}
	r.api.set("", errors.New("down"))
	r.src.Get(ctx)
	if r.src.Known() {
		t.Fatal("a fallback is not a panel answer")
	}
	r.api.set("US", nil) // garbage is a failure too
	r.clk.advance(time.Minute + time.Second)
	r.src.Get(ctx)
	if r.src.Known() {
		t.Fatal("an invalid answer is not a panel answer")
	}
	r.api.set("USD", nil)
	r.clk.advance(time.Minute + time.Second)
	r.src.Get(ctx)
	if !r.src.Known() {
		t.Fatal("not known after a success")
	}
	r.api.set("", errors.New("down again"))
	r.clk.advance(11 * time.Minute)
	r.src.Get(ctx)
	if !r.src.Known() {
		t.Fatal("known must stay true after a later failure")
	}
}

// gateReader blocks in BaseCurrency until released, like a real client does until its context ends.
type gateReader struct {
	entered chan struct{}
	release chan struct{}
}

func (g *gateReader) BaseCurrency(ctx context.Context) (string, error) {
	g.entered <- struct{}{}
	select {
	case <-g.release:
		return "USD", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestPanelRefreshSurvivesACancelledCaller(t *testing.T) {
	r := newRig("RUB")
	g := &gateReader{entered: make(chan struct{}, 1), release: make(chan struct{})}
	r.src.API = g
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go func() { done <- r.src.Get(ctx) }()
	<-g.entered
	cancel() // the WebApp client went away mid-call
	close(g.release)
	select {
	case got := <-done:
		if got != "USD" {
			t.Fatalf("got %q: the refresh must not fail with its caller", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Get did not return")
	}
	if !r.src.Known() {
		t.Error("the panel's answer was lost")
	}
	if n := strings.Count(r.logs.String(), "level=WARN"); n != 0 {
		t.Errorf("misleading warning: %s", r.logs.String())
	}
	// Cached: no new panel call, no backoff streak.
	if got := r.src.Get(context.Background()); got != "USD" || len(g.entered) != 0 {
		t.Errorf("got %q, entered %d", got, len(g.entered))
	}
}
