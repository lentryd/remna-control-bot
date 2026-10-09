package telegram

import (
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
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
