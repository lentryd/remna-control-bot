package telegram

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

func TestScrubURL(t *testing.T) {
	const token = "123:SECRET"
	in := &url.Error{Op: "Get", URL: "https://api.telegram.org/file/bot" + token + "/photos/1.jpg", Err: io.ErrUnexpectedEOF}
	got := scrubURL(in)
	if strings.Contains(got.Error(), token) || strings.Contains(got.Error(), "api.telegram.org") {
		t.Fatalf("leaks URL: %v", got)
	}
	if !strings.Contains(got.Error(), "Get") || !errors.Is(got, io.ErrUnexpectedEOF) {
		t.Fatalf("lost the failure: %v", got)
	}
	plain := errors.New("x")
	if scrubURL(plain) != plain {
		t.Fatal("non-url errors pass through")
	}
}

type closeSpy struct{ closed bool }

func (c *closeSpy) Close() error { c.closed = true; return nil }

func TestReceiptBodyCloseCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	spy := &closeSpy{}
	rb := &receiptBody{Reader: strings.NewReader("x"), body: spy, cancel: cancel}
	if err := rb.Close(); err != nil || !spy.closed || ctx.Err() == nil {
		t.Fatalf("close must close the body and cancel the context: closed=%v ctx=%v err=%v", spy.closed, ctx.Err(), err)
	}
}

func newReceiptBot(t *testing.T, h http.Handler) *Bot {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b, err := New(context.Background(), "123:ABC", Deps{
		API: vpnctl.NewFake(), WebAppURL: "https://x/bot", Admins: []int64{1}, Notices: st, APIBase: srv.URL,
		IsAdmin: func(int64) bool { return true }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReceiptStalledBodyTimesOut(t *testing.T) {
	old := receiptTimeout
	receiptTimeout = 300 * time.Millisecond
	t.Cleanup(func() { receiptTimeout = old })
	b := newReceiptBot(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"b","username":"vpnbot"}}`)
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			io.WriteString(w, `{"ok":true,"result":{"file_id":"f","file_path":"photos/1.jpg"}}`)
		case !strings.Contains(r.URL.Path, "/file/"):
			io.WriteString(w, `{"ok":true,"result":true}`)
		default: // the file: headers, a few bytes, then silence
			w.Header().Set("Content-Type", "image/jpeg")
			io.WriteString(w, "abc")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	rc, ct, err := b.Receipt(context.Background(), "f")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if ct != "image/jpeg" {
		t.Fatalf("content type %q", ct)
	}
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(rc); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a stalled body must fail, not look complete")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled body was never cut off")
	}
}
