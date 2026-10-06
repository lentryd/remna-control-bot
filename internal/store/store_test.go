package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAwaitingLink(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sub", "bot.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if on, _ := s.AwaitingLink(ctx, 1); on {
		t.Fatal("fresh store: want false")
	}
	if err := s.SetAwaitingLink(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAwaitingLink(ctx, 1, true); err != nil { // idempotent
		t.Fatal(err)
	}
	s.Close()

	s, _ = Open(path) // survives restart
	defer s.Close()
	if on, _ := s.AwaitingLink(ctx, 1); !on {
		t.Fatal("want true after reopen")
	}
	s.SetAwaitingLink(ctx, 1, false)
	if on, _ := s.AwaitingLink(ctx, 1); on {
		t.Fatal("want false after clear")
	}
}
