// Package store keeps the bot's own state: who is expected to send a
// subscription link, which notifications were already sent, payment orders,
// what a chat is expected to send next and where each admin's order card is.
// Customer data lives in vpn-control.
package store

import (
	"context"
	"database/sql"
	"errors"
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
		sent_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS orders (
		id TEXT PRIMARY KEY,
		customer_id INTEGER NOT NULL,
		chat_id INTEGER NOT NULL,
		kind TEXT NOT NULL,
		item_kind TEXT NOT NULL DEFAULT '',
		item_id INTEGER NOT NULL DEFAULT 0,
		tariff_id INTEGER NOT NULL DEFAULT 0,
		months INTEGER NOT NULL DEFAULT 0,
		days INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL,
		price REAL NOT NULL,
		from_balance REAL NOT NULL,
		to_pay REAL NOT NULL,
		received REAL,
		method TEXT NOT NULL,
		status TEXT NOT NULL,
		receipt_kind TEXT NOT NULL DEFAULT '',
		receipt_file_id TEXT NOT NULL DEFAULT '',
		comment TEXT NOT NULL DEFAULT '',
		reject_reason TEXT NOT NULL DEFAULT '',
		admin TEXT NOT NULL DEFAULT '',
		decision TEXT NOT NULL DEFAULT '',
		panel_payment_id INTEGER NOT NULL DEFAULT 0,
		result_until TIMESTAMP,
		error TEXT NOT NULL DEFAULT '',
		external_id TEXT NOT NULL DEFAULT '',
		pay_url TEXT NOT NULL DEFAULT '',
		pay_amount REAL NOT NULL DEFAULT 0,
		pay_currency TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL);
	CREATE INDEX IF NOT EXISTS orders_customer ON orders (customer_id, created_at);
	CREATE INDEX IF NOT EXISTS orders_status ON orders (status, created_at);
	CREATE TABLE IF NOT EXISTS chat_state (
		chat_id INTEGER PRIMARY KEY,
		kind TEXT NOT NULL,
		order_id TEXT NOT NULL);
	CREATE TABLE IF NOT EXISTS order_notices (
		order_id TEXT NOT NULL,
		admin_id INTEGER NOT NULL,
		msg_id INTEGER NOT NULL,
		PRIMARY KEY (order_id, admin_id));
	CREATE TABLE IF NOT EXISTS user_lang (
		tg_id INTEGER PRIMARY KEY,
		lang TEXT NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	if err := addColumn(db, "orders", "decision", `TEXT NOT NULL DEFAULT ''`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// addColumn brings a table created by an older version up to date; a no-op
// when the column is already there.
func addColumn(db *sql.DB, table, col, def string) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, col).Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + col + ` ` + def)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

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

// SetLang remembers the Telegram language of a person; an empty lang changes nothing.
func (s *Store) SetLang(ctx context.Context, tgID int64, lang string) error {
	if lang == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_lang(tg_id, lang) VALUES(?, ?) ON CONFLICT(tg_id) DO UPDATE SET lang = excluded.lang`, tgID, lang)
	return err
}

// Lang returns the remembered language, or "" when the person is unknown.
func (s *Store) Lang(ctx context.Context, tgID int64) (string, error) {
	var lang string
	err := s.db.QueryRowContext(ctx, `SELECT lang FROM user_lang WHERE tg_id = ?`, tgID).Scan(&lang)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return lang, err
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

// ChatState is what the next message in a chat means (e.g. a receipt for an order).
type ChatState struct {
	Kind    string
	OrderID string
}

func (s *Store) SetChatState(ctx context.Context, chatID int64, st ChatState) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO chat_state (chat_id, kind, order_id) VALUES (?, ?, ?)`, chatID, st.Kind, st.OrderID)
	return err
}

func (s *Store) GetChatState(ctx context.Context, chatID int64) (ChatState, bool, error) {
	var st ChatState
	err := s.db.QueryRowContext(ctx, `SELECT kind, order_id FROM chat_state WHERE chat_id = ?`, chatID).Scan(&st.Kind, &st.OrderID)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	return st, err == nil, err
}

func (s *Store) ClearChatState(ctx context.Context, chatID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chat_state WHERE chat_id = ?`, chatID)
	return err
}

// SaveNotice remembers which message carries an order's card in an admin's chat.
func (s *Store) SaveNotice(ctx context.Context, orderID string, adminID int64, msgID int) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO order_notices (order_id, admin_id, msg_id) VALUES (?, ?, ?)`, orderID, adminID, msgID)
	return err
}

// Notices maps an admin id to the message that carries the order's card in their chat.
func (s *Store) Notices(ctx context.Context, orderID string) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT admin_id, msg_id FROM order_notices WHERE order_id = ?`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var admin int64
		var msg int
		if err := rows.Scan(&admin, &msg); err != nil {
			return nil, err
		}
		out[admin] = msg
	}
	return out, rows.Err()
}
