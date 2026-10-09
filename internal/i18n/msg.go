package i18n

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// Msg is a catalog message kept for later: built where the reader is not known (an order title, an admin note),
// stored as a string and rendered in each reader's language. Args are strings, integers or nested Msgs; a whole
// float comes back from storage as an integer, so amounts are passed already formatted.
type Msg struct {
	Key  string `json:"k"`
	Args []any  `json:"a,omitempty"`
}

func M(key string, args ...any) Msg { return Msg{Key: key, Args: args} }

// msgPrefix marks a stored Msg; any other stored string is shown as it is (rows written before localization,
// raw panel errors, people's own text).
const msgPrefix = "i18n:"

// String is the stored form of m.
func (m Msg) String() string {
	b, err := json.Marshal(m)
	if err != nil { // only unsupported argument types; callers pass strings and numbers
		return m.Key
	}
	return msgPrefix + string(b)
}

// Render formats m, nested messages included, in l's language.
func (l Localizer) Render(m Msg) string {
	args := make([]any, len(m.Args))
	for i, a := range m.Args {
		if n, ok := a.(Msg); ok {
			a = l.Render(n)
		}
		args[i] = a
	}
	return l.T(m.Key, args...)
}

// Text renders a stored Msg in l's language; any other string comes back unchanged.
func (l Localizer) Text(stored string) string {
	if m, ok := parseMsg(stored); ok {
		return l.Render(m)
	}
	return stored
}

func parseMsg(s string) (Msg, bool) {
	raw, ok := strings.CutPrefix(s, msgPrefix)
	if !ok {
		return Msg{}, false
	}
	var w struct {
		Key  string            `json:"k"`
		Args []json.RawMessage `json:"a"`
	}
	if json.Unmarshal([]byte(raw), &w) != nil || w.Key == "" {
		return Msg{}, false
	}
	m := Msg{Key: w.Key}
	for _, r := range w.Args {
		a, ok := parseArg(r)
		if !ok {
			return Msg{}, false
		}
		m.Args = append(m.Args, a)
	}
	return m, true
}

func parseArg(r json.RawMessage) (any, bool) {
	r = bytes.TrimSpace(r)
	if len(r) > 0 && r[0] == '{' {
		return parseMsg(msgPrefix + string(r))
	}
	d := json.NewDecoder(bytes.NewReader(r))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return nil, false
	}
	if n, ok := v.(json.Number); ok { // %d needs an integer back, %.2f a float
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		f, err := n.Float64()
		return f, err == nil
	}
	return v, true
}

type ctxKey struct{}

// NewContext carries the Localizer of the person a request is for.
func NewContext(ctx context.Context, l Localizer) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext is the Localizer NewContext stored, or English.
func FromContext(ctx context.Context) Localizer {
	l, _ := ctx.Value(ctxKey{}).(Localizer)
	return l
}
