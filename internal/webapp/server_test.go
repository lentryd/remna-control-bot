package webapp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/vpnctl"
	"vpn-control-bot/internal/webapp/initdata"
)

const token = "123:ABC"

func ptr[T any](v T) *T { return &v }

func app(f *vpnctl.Fake) *Deps {
	return &Deps{
		API: f, Codec: refcode.New("s"), BotToken: token, BotUsername: "vpnbot", Path: "/bot",
		Static: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}, "assets/a.js": {Data: []byte("js")}},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func tma(id int64) string {
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(time.Now().Unix(), 10))
	v.Set("user", `{"id":`+strconv.FormatInt(id, 10)+`,"first_name":"Ann"}`)
	return "tma " + initdata.Sign(v, token)
}

func get(t *testing.T, d *Deps, path, auth string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := New(*d).Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestMe(t *testing.T) {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 1, Name: "Ann", TelegramID: ptr(int64(42)), Notes: "SECRET", Balance: 10})
	d := app(f)

	if code, _ := get(t, d, "/bot/api/me", ""); code != 401 {
		t.Fatalf("no auth: %d", code)
	}
	if code, _ := get(t, d, "/bot/api/me", "tma garbage"); code != 401 {
		t.Fatalf("bad auth: %d", code)
	}
	if code, _ := get(t, d, "/bot/api/me", tma(7)); code != 403 {
		t.Fatalf("unknown user: %d", code)
	}
	code, body := get(t, d, "/bot/api/me", tma(42))
	if code != 200 || strings.Contains(body, "SECRET") {
		t.Fatalf("me: %d %s", code, body)
	}
	var p struct {
		Name    string `json:"name"`
		RefLink string `json:"ref_link"`
	}
	json.Unmarshal([]byte(body), &p)
	if p.Name != "Ann" || !strings.HasPrefix(p.RefLink, "https://t.me/vpnbot?start=ref_") {
		t.Fatalf("profile %+v", p)
	}
}

func TestArchivedAndDown(t *testing.T) {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 1, TelegramID: ptr(int64(42)), Archived: true})
	d := app(f)
	if code, _ := get(t, d, "/bot/api/me", tma(42)); code != 403 {
		t.Fatalf("archived: %d", code)
	}
	f.Err = vpnctl.ErrUnavailable
	if code, _ := get(t, d, "/bot/api/me", tma(42)); code != 503 {
		t.Fatalf("down: %d", code)
	}
}

func TestTariffsAndStatic(t *testing.T) {
	f := vpnctl.NewFake()
	f.Add(vpnctl.Customer{ID: 1, TelegramID: ptr(int64(42))})
	f.Tariffs = []vpnctl.Tariff{{ID: 1, Kind: "base", Name: "Std", Active: true}, {ID: 2, Name: "Off"}}
	d := app(f)
	code, body := get(t, d, "/bot/api/tariffs", tma(42))
	if code != 200 || !strings.Contains(body, "Std") || strings.Contains(body, "Off") {
		t.Fatalf("tariffs: %d %s", code, body)
	}
	if code, _ := get(t, d, "/bot", ""); code != 301 {
		t.Fatalf("/bot redirect: %d", code)
	}
	if code, body := get(t, d, "/bot/", ""); code != 200 || !strings.Contains(body, "app") {
		t.Fatalf("index: %d %s", code, body)
	}
	if code, body := get(t, d, "/bot/assets/a.js", ""); code != 200 || body != "js" {
		t.Fatalf("asset: %d %s", code, body)
	}
	if code, _ := get(t, d, "/other", ""); code != 404 {
		t.Fatalf("outside path: %d", code)
	}
}
