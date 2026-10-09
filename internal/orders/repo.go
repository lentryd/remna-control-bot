package orders

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const cols = `id, customer_id, chat_id, kind, item_kind, item_id, tariff_id, months, days, title, price, from_balance, to_pay,
	received, method, status, receipt_kind, receipt_file_id, comment, reject_reason, admin, admin_msg_id, decision, panel_payment_id,
	result_until, error, external_id, pay_url, pay_amount, pay_currency, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scan(r scanner) (*Order, error) {
	var o Order
	var received sql.NullFloat64
	var until sql.NullTime
	err := r.Scan(&o.ID, &o.CustomerID, &o.ChatID, &o.Kind, &o.ItemKind, &o.ItemID, &o.TariffID, &o.Months, &o.Days,
		&o.Title, &o.Price, &o.FromBalance, &o.ToPay, &received, &o.Method, &o.Status, &o.ReceiptKind, &o.ReceiptFileID,
		&o.Comment, &o.RejectReason, &o.Admin, &o.AdminMsgID, &o.Decision, &o.PanelPaymentID, &until, &o.Error,
		&o.ExternalID, &o.PayURL, &o.PayAmount, &o.PayCurrency, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if received.Valid {
		o.Received = &received.Float64
	}
	if until.Valid {
		o.ResultUntil = &until.Time
	}
	return &o, err
}

func (s *Service) insert(ctx context.Context, o *Order) error {
	now := s.Now()
	o.ID, o.CreatedAt, o.UpdatedAt = s.newID(), now, now
	_, err := s.DB.ExecContext(ctx, `INSERT INTO orders (id, customer_id, chat_id, kind, item_kind, item_id, tariff_id, months, days,
		title, price, from_balance, to_pay, method, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.CustomerID, o.ChatID, o.Kind, o.ItemKind, o.ItemID, o.TariffID, o.Months, o.Days,
		o.Title, o.Price, o.FromBalance, o.ToPay, o.Method, o.Status, now, now)
	return err
}

// newID is a UUID, so order ids can't be guessed or counted.
func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return uuid.NewString()
}

func (s *Service) Get(ctx context.Context, id string) (*Order, error) {
	return scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM orders WHERE id = ?`, id))
}

// ForCustomer is Get limited to the customer's own orders.
func (s *Service) ForCustomer(ctx context.Context, customerID int, id string) (*Order, error) {
	return scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM orders WHERE id = ? AND customer_id = ?`, id, customerID))
}

// List is the customer's orders, newest first, without drafts nobody paid
// for (expired/cancelled ones stay — they explain a missing renewal).
func (s *Service) List(ctx context.Context, customerID, limit, offset int) ([]Order, int, error) {
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE customer_id = ? AND status != ?`, customerID, StatusNew).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM orders WHERE customer_id = ? AND status != ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		customerID, StatusNew, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *o)
	}
	return out, total, rows.Err()
}

// ListAll is every customer's orders for the admins, newest first. With no
// statuses it hides drafts nobody paid for (StatusNew), like List does.
func (s *Service) ListAll(ctx context.Context, statuses []Status, limit, offset int) ([]Order, int, error) {
	where, args := `status != ?`, []any{StatusNew}
	if len(statuses) > 0 {
		where = `status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		args = args[:0]
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM orders WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *o)
	}
	return out, total, rows.Err()
}

// move changes the status only from one of the expected ones; set holds
// extra columns to write in the same statement ("col = ?" → value).
func (s *Service) move(ctx context.Context, id string, from []Status, to Status, set map[string]any) error {
	q := `UPDATE orders SET status = ?, updated_at = ?`
	args := []any{to, s.Now()}
	for _, col := range slices.Sorted(maps.Keys(set)) {
		q += ", " + col + " = ?"
		args = append(args, set[col])
	}
	q += ` WHERE id = ? AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(from)), ",") + `)`
	args = append(args, id)
	for _, f := range from {
		args = append(args, f)
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStateChanged
	}
	return nil
}

// set writes columns without a status change (e.g. the admin card id).
func (s *Service) set(ctx context.Context, id string, cols map[string]any) error {
	o, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.move(ctx, id, []Status{o.Status}, o.Status, cols)
}
