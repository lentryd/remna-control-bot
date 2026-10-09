package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrUnknownSection = errors.New("settings: unknown section")

// ValidationError is a rejected save: the admin's input, not a server fault.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

const maskDots = "••••"

// Mask hides a secret for display, keeping its last four characters when it is long enough to stay safe.
func Mask(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 8 {
		return maskDots
	}
	return maskDots + string(r[len(r)-4:])
}

type Store struct {
	db   *sql.DB
	box  *box
	mu   sync.Mutex // serializes Put and guards subs/lost
	snap atomic.Pointer[Snapshot]
	subs []func(Snapshot)
	lost []string
}

// Open creates the table if needed and loads every stored section. A secret
// that can no longer be decrypted (REF_SECRET changed) loads empty and the
// section is reported by Lost instead of failing the start.
func Open(ctx context.Context, db *sql.DB, secret string) (*Store, error) {
	b, err := newBox(secret)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL)`); err != nil {
		return nil, err
	}
	s := &Store{db: db, box: b}
	snap := Defaults()
	for _, key := range Keys {
		var raw string
		err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sec := newSection(key)
		if err := json.Unmarshal([]byte(raw), sec); err != nil {
			return nil, fmt.Errorf("settings: stored %s is unreadable: %w", key, err)
		}
		lost := false
		for _, p := range sec.Secrets() {
			plain, err := b.open(*p)
			if err != nil {
				lost = true
			}
			*p = plain
		}
		if lost {
			s.lost = append(s.lost, key)
		}
		snap = snap.With(sec)
	}
	s.snap.Store(&snap)
	return s, nil
}

func (s *Store) Snapshot() Snapshot { return *s.snap.Load() }

// Lost lists the sections whose secrets could not be decrypted at start and were not re-entered since.
func (s *Store) Lost() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.lost)
}

// OnChange registers fn to be called, in order, after every successful Put. Register before serving requests.
func (s *Store) OnChange(fn func(Snapshot)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}

// View is the section as the admin API shows it: secrets masked.
func (s *Store) View(key string) (Section, bool) {
	sec := s.Snapshot().section(key)
	if sec == nil {
		return nil, false
	}
	b, _ := json.Marshal(sec)
	c := newSection(key)
	_ = json.Unmarshal(b, c)
	for _, p := range c.Secrets() {
		*p = Mask(*p)
	}
	return c, true
}

// Put replaces one section with the JSON body. Fields missing from the body
// take their defaults; a secret that is empty or still masked keeps the
// stored value. The snapshot changes only when the save succeeds.
func (s *Store) Put(ctx context.Context, key string, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.Snapshot()
	old := cur.section(key)
	if old == nil {
		return ErrUnknownSection
	}
	next := newSection(key)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(next); err != nil {
		return &ValidationError{"invalid body: " + err.Error()}
	}
	oldSecrets := old.Secrets()
	for i, p := range next.Secrets() {
		if *p == "" || strings.HasPrefix(*p, maskDots) {
			*p = *oldSecrets[i]
		}
	}
	if err := next.Validate(); err != nil {
		return &ValidationError{err.Error()}
	}
	stored, err := s.seal(next)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, stored, time.Now()); err != nil {
		return err
	}
	snap := cur.With(next)
	s.snap.Store(&snap)
	s.lost = slices.DeleteFunc(s.lost, func(k string) bool { return k == key })
	for _, fn := range s.subs {
		fn(snap)
	}
	return nil
}

// seal marshals sec with its secrets encrypted, leaving sec itself in plain text.
func (s *Store) seal(sec Section) (string, error) {
	ptrs := sec.Secrets()
	plain := make([]string, len(ptrs))
	for i, p := range ptrs {
		plain[i] = *p
	}
	defer func() {
		for i, p := range ptrs {
			*p = plain[i]
		}
	}()
	for _, p := range ptrs {
		enc, err := s.box.seal(*p)
		if err != nil {
			return "", err
		}
		*p = enc
	}
	b, err := json.Marshal(sec)
	return string(b), err
}
