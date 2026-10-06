// Package store keeps the bot's own state: who is expected to send a
// subscription link and which notifications were already sent. Customer
// data lives in vpn-control.
package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS awaiting_link (
		tg_id INTEGER PRIMARY KEY,
		since TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS notices (
		key TEXT PRIMARY KEY,
		sent_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SetAwaitingLink(ctx context.Context, tgID int64, on bool) error {
	q := `DELETE FROM awaiting_link WHERE tg_id = ?`
	if on {
		q = `INSERT OR IGNORE INTO awaiting_link (tg_id) VALUES (?)`
	}
	_, err := s.db.ExecContext(ctx, q, tgID)
	return err
}

func (s *Store) AwaitingLink(ctx context.Context, tgID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM awaiting_link WHERE tg_id = ?`, tgID).Scan(&n)
	return n > 0, err
}

// NoticeSent reports whether a notification with this key was sent.
func (s *Store) NoticeSent(ctx context.Context, key string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notices WHERE key = ?`, key).Scan(&n)
	return n > 0, err
}

func (s *Store) MarkNoticeSent(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO notices (key) VALUES (?)`, key)
	return err
}
