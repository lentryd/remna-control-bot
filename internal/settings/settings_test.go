package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"remna-control-bot/internal/config"
	"remna-control-bot/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st.DB()
}

func openStore(t *testing.T, db *sql.DB, secret string) *Store {
	t.Helper()
	s, err := Open(context.Background(), db, secret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const yooBody = `{"enabled":true,"shop_id":"123","secret_key":"live_supersecretvalue","vat_code":1}`

func TestDefaults(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	g := s.Snapshot().General()
	if g.Mode != config.ModeClosed || g.PayMode != config.PayMixed || g.NotifyMinutes != 60 || g.TrafficAlertPct != 80 || g.OrderTTLHours != 48 {
		t.Fatalf("defaults: %+v", g)
	}
	if !slices.Equal(g.RemindDescending(), []int{3, 1}) {
		t.Fatalf("remind: %v", g.RemindDescending())
	}
	if s.Snapshot().Manual().Active() || s.Snapshot().YooKassa().Active() || s.Snapshot().Stars().Active() {
		t.Fatal("everything must start off")
	}
}

func TestPutRoundTripEncrypts(t *testing.T) {
	db := openDB(t)
	s := openStore(t, db, "ref-secret")
	if err := s.Put(context.Background(), KeyYooKassa, []byte(yooBody)); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, KeyYooKassa).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "supersecret") {
		t.Fatalf("secret stored in clear: %s", raw)
	}
	again := openStore(t, db, "ref-secret")
	y := again.Snapshot().YooKassa()
	if !y.Active() || y.SecretKey != "live_supersecretvalue" || y.ShopID != "123" {
		t.Fatalf("reloaded: %+v", y)
	}
	v, _ := again.View(KeyYooKassa)
	if got := v.(*YooKassa).SecretKey; got != "••••alue" {
		t.Fatalf("view must mask, got %q", got)
	}
}

func TestSecretKeptOnMask(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	ctx := context.Background()
	if err := s.Put(ctx, KeyYooKassa, []byte(yooBody)); err != nil {
		t.Fatal(err)
	}
	for _, echoed := range []string{"••••alue", ""} {
		body, _ := json.Marshal(map[string]any{"enabled": true, "shop_id": "999", "secret_key": echoed, "vat_code": 2})
		if err := s.Put(ctx, KeyYooKassa, body); err != nil {
			t.Fatalf("%q: %v", echoed, err)
		}
		y := s.Snapshot().YooKassa()
		if y.SecretKey != "live_supersecretvalue" || y.ShopID != "999" || y.VATCode != 2 {
			t.Fatalf("%q: %+v", echoed, y)
		}
	}
	body := `{"enabled":true,"shop_id":"999","secret_key":"brand-new-key","vat_code":2}`
	if err := s.Put(ctx, KeyYooKassa, []byte(body)); err != nil || s.Snapshot().YooKassa().SecretKey != "brand-new-key" {
		t.Fatalf("replace: %v %+v", err, s.Snapshot().YooKassa())
	}
}

func TestValidation(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	ctx := context.Background()
	var ve *ValidationError
	for name, c := range map[string]struct{ key, body string }{
		"enabled without keys": {KeyYooKassa, `{"enabled":true,"vat_code":1}`},
		"unknown field":        {KeyCryptoBot, `{"enabled":false,"nope":1}`},
		"not json":             {KeyCryptoBot, `{`},
		"bad mode":             {KeyGeneral, `{"mode":"wide","pay_mode":"mixed","notify_minutes":60,"traffic_alert_pct":80,"remind_days":[3],"order_ttl_hours":48}`},
		"bad currency":         {KeyGeneral, `{"mode":"open","base_currency":"rub","pay_mode":"mixed","notify_minutes":60,"traffic_alert_pct":80,"remind_days":[3],"order_ttl_hours":48}`},
		"stars rate":           {KeyStars, `{"enabled":true,"rate":0}`},
		"manual without text":  {KeyManual, `{"enabled":true,"details":"  "}`},
		"platega version":      {KeyPlatega, `{"enabled":false,"payment_method":2,"api_version":"v9"}`},
	} {
		if err := s.Put(ctx, c.key, []byte(c.body)); !errors.As(err, &ve) {
			t.Errorf("%s: want ValidationError, got %v", name, err)
		}
	}
	if err := s.Put(ctx, "pay.nope", []byte(`{}`)); !errors.Is(err, ErrUnknownSection) {
		t.Fatalf("unknown section: %v", err)
	}
	if s.Snapshot().YooKassa().Enabled {
		t.Fatal("a rejected save must not change the snapshot")
	}
	// A disabled provider may be saved half-filled.
	if err := s.Put(ctx, KeyHeleket, []byte(`{"enabled":false,"merchant_id":"m"}`)); err != nil {
		t.Fatalf("draft: %v", err)
	}
	if s.Snapshot().Heleket().Complete() {
		t.Fatal("half-filled provider is not complete")
	}
}

func TestLostSecrets(t *testing.T) {
	db := openDB(t)
	s := openStore(t, db, "old")
	if err := s.Put(context.Background(), KeyYooKassa, []byte(yooBody)); err != nil {
		t.Fatal(err)
	}
	moved := openStore(t, db, "new") // REF_SECRET changed
	if !slices.Equal(moved.Lost(), []string{KeyYooKassa}) {
		t.Fatalf("lost: %v", moved.Lost())
	}
	y := moved.Snapshot().YooKassa()
	if y.SecretKey != "" || y.Active() {
		t.Fatalf("lost secret must load empty and inactive: %+v", y)
	}
	body := `{"enabled":true,"shop_id":"123","secret_key":"fresh","vat_code":1}`
	if err := moved.Put(context.Background(), KeyYooKassa, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if len(moved.Lost()) != 0 || !moved.Snapshot().YooKassa().Active() {
		t.Fatal("re-entering the secret must clear the warning")
	}
}

func TestOnChange(t *testing.T) {
	s := openStore(t, openDB(t), "s")
	var got []Snapshot
	s.OnChange(func(sn Snapshot) { got = append(got, sn) })
	if err := s.Put(context.Background(), KeyManual, []byte(`{"enabled":true,"details":"Card 1"}`)); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Manual().Active() || got[0].Manual().Details != "Card 1" {
		t.Fatalf("subscriber: %+v", got)
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{"": "", "abc": "••••", "12345678": "••••", "123456789": "••••6789"} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}
