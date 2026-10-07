package store

import (
	"context"
	"database/sql"
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

func TestNoticesSentOnce(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if sent, _ := s.NoticeSent(ctx, "k"); sent {
		t.Fatal("fresh store: want not sent")
	}
	if err := s.MarkNoticeSent(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkNoticeSent(ctx, "k"); err != nil { // idempotent
		t.Fatal(err)
	}
	if sent, _ := s.NoticeSent(ctx, "k"); !sent {
		t.Fatal("want sent")
	}
}

func TestChatStateAndPrompts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, ok, _ := s.GetChatState(ctx, 1); ok {
		t.Fatal("state before set")
	}
	s.SetChatState(ctx, 1, ChatState{Kind: "receipt", OrderID: "9"})
	s.SetChatState(ctx, 1, ChatState{Kind: "receipt", OrderID: "10"}) // replaces
	if st, ok, err := s.GetChatState(ctx, 1); !ok || err != nil || st.OrderID != "10" {
		t.Fatalf("state %+v %v %v", st, ok, err)
	}
	s.ClearChatState(ctx, 1)
	if _, ok, _ := s.GetChatState(ctx, 1); ok {
		t.Fatal("state after clear")
	}
	s.SavePrompt(ctx, -100, 55, Prompt{Kind: "reject", OrderID: "3"})
	if p, ok, _ := s.TakePrompt(ctx, -100, 55); !ok || p.OrderID != "3" || p.Kind != "reject" {
		t.Fatalf("prompt %+v", p)
	}
	if _, ok, _ := s.TakePrompt(ctx, -100, 55); ok {
		t.Fatal("prompt taken twice")
	}
	if _, err := s.DB().Exec(`SELECT id, status FROM orders`); err != nil {
		t.Fatalf("orders table: %v", err)
	}
}

func TestMigratesDecisionColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER NOT NULL, chat_id INTEGER NOT NULL, kind TEXT NOT NULL,
		item_kind TEXT NOT NULL DEFAULT '', item_id INTEGER NOT NULL DEFAULT 0, tariff_id INTEGER NOT NULL DEFAULT 0,
		months INTEGER NOT NULL DEFAULT 0, days INTEGER NOT NULL DEFAULT 0, title TEXT NOT NULL, price REAL NOT NULL,
		from_balance REAL NOT NULL, to_pay REAL NOT NULL, received REAL, method TEXT NOT NULL, status TEXT NOT NULL,
		receipt_kind TEXT NOT NULL DEFAULT '', receipt_file_id TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '',
		reject_reason TEXT NOT NULL DEFAULT '', admin TEXT NOT NULL DEFAULT '', admin_msg_id INTEGER NOT NULL DEFAULT 0,
		panel_payment_id INTEGER NOT NULL DEFAULT 0, result_until TIMESTAMP, error TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL);
		INSERT INTO orders (customer_id, chat_id, kind, title, price, from_balance, to_pay, method, status, created_at, updated_at)
		VALUES (5, 77, 'renew', 't', 1, 0, 1, 'manual', 'failed', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 { // idempotent
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var d string
		if err := s.DB().QueryRow(`SELECT decision FROM orders WHERE id = 1`).Scan(&d); err != nil || d != "" {
			t.Fatalf("%q %v", d, err)
		}
		s.Close()
	}
}

func TestOrdersProviderColumns(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB().Exec(`SELECT external_id, pay_url, pay_amount, pay_currency FROM orders`); err != nil {
		t.Fatalf("provider columns: %v", err)
	}
}
