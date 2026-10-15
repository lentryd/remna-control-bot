package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"remna-control-bot/internal/refcode"
	"remna-control-bot/internal/remnactl"
	"remna-control-bot/internal/webapp/initdata"
)

const token = "123:ABC"

func ptr[T any](v T) *T { return &v }

func app(f *remnactl.Fake) *Deps {
	return &Deps{
		API: f, Codec: refcode.New("s"), BotToken: token, BotUsername: "remnabot", Path: "/bot",
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
	f := remnactl.NewFake()
	f.Add(remnactl.Customer{ID: 1, Name: "Ann", TelegramID: ptr(int64(42)), Notes: "SECRET", Balance: 10})
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
		IsAdmin *bool  `json:"is_admin"`
	}
	json.Unmarshal([]byte(body), &p)
	if p.IsAdmin == nil || *p.IsAdmin {
		t.Fatalf("is_admin must be present and false for a customer: %s", body)
	}
	if p.Name != "Ann" || !strings.HasPrefix(p.RefLink, "https://t.me/remnabot?start=ref_") {
		t.Fatalf("profile %+v", p)
	}
}

func TestMeCurrency(t *testing.T) {
	f := remnactl.NewFake()
	f.Add(remnactl.Customer{ID: 1, Name: "Ann", TelegramID: ptr(int64(42))})
	d := app(f)
	if _, body := get(t, d, "/bot/api/me", tma(42)); !strings.Contains(body, `"currency":"RUB"`) {
		t.Fatalf("nil Currency should fall back to RUB: %s", body)
	}
	d.Currency = func(context.Context) string { return "USD" }
	if _, body := get(t, d, "/bot/api/me", tma(42)); !strings.Contains(body, `"currency":"USD"`) {
		t.Fatalf("currency from the source: %s", body)
	}
}

func TestArchivedAndDown(t *testing.T) {
	f := remnactl.NewFake()
	f.Add(remnactl.Customer{ID: 1, TelegramID: ptr(int64(42)), Archived: true})
	d := app(f)
	if code, _ := get(t, d, "/bot/api/me", tma(42)); code != 403 {
		t.Fatalf("archived: %d", code)
	}
	f.Err = remnactl.ErrUnavailable
	if code, _ := get(t, d, "/bot/api/me", tma(42)); code != 503 {
		t.Fatalf("down: %d", code)
	}
}

func TestTariffsAndStatic(t *testing.T) {
	f := remnactl.NewFake()
	f.Add(remnactl.Customer{ID: 1, TelegramID: ptr(int64(42))})
	f.Tariffs = []remnactl.Tariff{{ID: 1, Kind: "base", Name: "Std", Active: true, MonthlyPrice: 100}, {ID: 2, Name: "Off"}}
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

type fakeLangs struct {
	got  map[int64]string
	fail bool
}

func (f *fakeLangs) SetLang(_ context.Context, id int64, lang string) error {
	if f.got == nil {
		f.got = map[int64]string{}
	}
	f.got[id] = lang
	if f.fail {
		return errors.New("db down")
	}
	return nil
}

func tmaLang(id int64, lang string) string {
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(time.Now().Unix(), 10))
	v.Set("user", `{"id":`+strconv.FormatInt(id, 10)+`,"first_name":"Ann","language_code":"`+lang+`"}`)
	return "tma " + initdata.Sign(v, token)
}

func TestAuthRemembersLanguage(t *testing.T) {
	f := remnactl.NewFake()
	f.Add(remnactl.Customer{ID: 5, Name: "Ann", TelegramID: ptr(int64(77))})
	d := app(f)
	l := &fakeLangs{}
	d.Langs = l
	if code, _ := get(t, d, "/bot/api/me", tmaLang(77, "ru")); code != 200 || l.got[77] != "ru" {
		t.Fatalf("code=%d got=%v", code, l.got)
	}
	get(t, d, "/bot/api/me", tmaLang(78, "xx-YY")) // unsupported: stored as the base language
	if l.got[78] != "en" {
		t.Fatalf("got=%v", l.got)
	}
	l.got = map[int64]string{77: "ru"} // no language_code: nothing is stored, the remembered one survives
	if code, _ := get(t, d, "/bot/api/me", tma(77)); code != 200 || l.got[77] != "ru" || len(l.got) != 1 {
		t.Fatalf("code=%d got=%v", code, l.got)
	}
	l.got = nil
	if code, _ := get(t, d, "/bot/api/me", "tma bad"); code != 401 || len(l.got) != 0 {
		t.Fatalf("code=%d got=%v", code, l.got)
	}
	if code, _ := get(t, d, "/bot/api/me", ""); code != 401 || len(l.got) != 0 {
		t.Fatalf("code=%d got=%v", code, l.got)
	}
	l.fail = true // failing to remember never fails the request
	if code, _ := get(t, d, "/bot/api/me", tmaLang(77, "ru")); code != 200 {
		t.Fatalf("code=%d", code)
	}
}
