package webapp

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/payments"
	"remna-control-bot/internal/remnactl"
	"remna-control-bot/internal/settings"
	"remna-control-bot/internal/store"
	"remna-control-bot/internal/webapp/initdata"
)

const adminID = 900 // an admin who is not a panel customer

func adminApp(t *testing.T) (*Deps, *remnactl.Fake) {
	t.Helper()
	f := ordersFake()
	d := app(f)
	withOrders(t, f, d)
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if d.Settings, err = settings.Open(context.Background(), st.DB(), "ref"); err != nil {
		t.Fatal(err)
	}
	d.IsAdmin = func(id int64) bool { return id == adminID }
	d.PublicURL = "https://pub.example"
	return d, f
}

// pending creates a top-up order of 100 for customer 5 (telegram 77) and hands it to the admins.
func pending(t *testing.T, d *Deps, f *remnactl.Fake) string {
	t.Helper()
	ctx := context.Background()
	c, err := f.GetCustomer(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	o, err := d.Orders.Create(ctx, c, 77, orders.Request{Kind: orders.KindTopup, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Orders.RequestReceipt(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Orders.AttachReceipt(ctx, 77, orders.ReceiptText, "", "paid"); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func TestAdminAuth(t *testing.T) {
	d, _ := adminApp(t)
	for name, c := range map[string]struct {
		auth string
		want int
	}{
		"no header":             {"", 401},
		"garbage":               {"tma garbage", 401},
		"customer, not admin":   {tma(77), 403},
		"stranger":              {tma(5), 403},
		"admin, not a customer": {tma(adminID), 200},
	} {
		if code, body := get(t, d, "/bot/api/admin/me", c.auth); code != c.want {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	if code, _ := get(t, d, "/bot/api/admin/orders", tma(77)); code != 403 {
		t.Fatalf("a customer must not list orders: %d", code)
	}
	// The customer API still works for customers and still refuses non-customers.
	if code, _ := get(t, d, "/bot/api/me", tma(adminID)); code != 403 {
		t.Fatalf("admin who is not a customer on /me: %d", code)
	}
}

func TestAdminApproveTwice(t *testing.T) {
	d, f := adminApp(t)
	id := pending(t, d, f)

	code, body := do(t, d, "GET", "/bot/api/admin/orders?status=pending_review", tma(adminID), "")
	var list struct {
		Orders []struct {
			ID, Status string
		} `json:"orders"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || code != 200 || list.Total != 1 || list.Orders[0].ID != id {
		t.Fatalf("list %d %s", code, body)
	}
	if code, _ := do(t, d, "GET", "/bot/api/admin/orders?status=bogus", tma(adminID), ""); code != 400 {
		t.Fatalf("bad status filter: %d", code)
	}

	path := "/bot/api/admin/orders/" + id + "/approve"
	if code, body := do(t, d, "POST", path, tma(adminID), `{"decision":"full"}`); code != 200 || !strings.Contains(body, `"status":"approved"`) {
		t.Fatalf("approve %d %s", code, body)
	}
	if code, body := do(t, d, "POST", path, tma(adminID), `{"decision":"full"}`); code != 409 {
		t.Fatalf("second admin must get a conflict, got %d %s", code, body)
	}
	if code, _ := do(t, d, "POST", "/bot/api/admin/orders/not-a-uuid/approve", tma(adminID), `{"decision":"full"}`); code != 404 {
		t.Fatalf("bad id: %d", code)
	}
}

func TestAdminAmountAndReject(t *testing.T) {
	d, f := adminApp(t)
	id := pending(t, d, f)
	approve := "/bot/api/admin/orders/" + id + "/approve"
	// 60 received of 100: "full" is refused, "credit" works and records the corrected amount.
	if code, body := do(t, d, "POST", approve, tma(adminID), `{"decision":"full","amount":60}`); code != 409 || !strings.Contains(body, "underpaid") {
		t.Fatalf("underpaid %d %s", code, body)
	}
	code, body := do(t, d, "POST", approve, tma(adminID), `{"decision":"credit","amount":60}`)
	if code != 200 || !strings.Contains(body, `"status":"approved"`) || !strings.Contains(body, `"paid":60`) {
		t.Fatalf("credit %d %s", code, body)
	}

	id2 := pending(t, d, f)
	code, body = do(t, d, "POST", "/bot/api/admin/orders/"+id2+"/reject", tma(adminID), `{"reason":"no money"}`)
	if code != 200 || !strings.Contains(body, `"status":"rejected"`) || !strings.Contains(body, "no money") {
		t.Fatalf("reject %d %s", code, body)
	}
}

func TestAdminSettings(t *testing.T) {
	d, _ := adminApp(t)
	put := func(section, body string) (int, string) {
		return do(t, d, "PUT", "/bot/api/admin/settings/"+section, tma(adminID), body)
	}
	if code, body := put("pay.yookassa", `{"enabled":true,"vat_code":1}`); code != 422 || !strings.Contains(body, `"code":"invalid"`) {
		t.Fatalf("invalid %d %s", code, body)
	}
	if code, _ := put("pay.nope", `{}`); code != 404 {
		t.Fatalf("unknown section %d", code)
	}
	code, body := put("pay.yookassa", `{"enabled":true,"shop_id":"1","secret_key":"live_supersecretvalue","vat_code":1}`)
	if code != 200 || strings.Contains(body, "supersecret") || !strings.Contains(body, "••••alue") {
		t.Fatalf("save must answer masked: %d %s", code, body)
	}
	// The masked value comes back from the UI: the secret survives.
	if code, _ := put("pay.yookassa", `{"enabled":true,"shop_id":"2","secret_key":"••••alue","vat_code":1}`); code != 200 {
		t.Fatalf("echoed mask: %d", code)
	}
	if got := d.Settings.Snapshot().YooKassa(); got.SecretKey != "live_supersecretvalue" || got.ShopID != "2" {
		t.Fatalf("snapshot: %+v", got)
	}

	code, body = do(t, d, "GET", "/bot/api/admin/settings", tma(adminID), "")
	var got struct {
		Sections    map[string]map[string]any `json:"sections"`
		Lost        []string                  `json:"lost"`
		WebhookBase string                    `json:"webhook_base"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || code != 200 || len(got.Sections) != len(settings.Keys) ||
		got.WebhookBase != "https://pub.example" || strings.Contains(body, "supersecret") || !strings.Contains(body, `"lost":[]`) {
		t.Fatalf("get %d %s", code, body)
	}
	if code, _ := do(t, d, "GET", "/bot/api/admin/settings", tma(77), ""); code != 403 {
		t.Fatalf("customer on settings: %d", code)
	}
}

func TestAdminSettingsTest(t *testing.T) {
	d, _ := adminApp(t)
	test := func(section string) (int, string) {
		return do(t, d, "POST", "/bot/api/admin/settings/"+section+"/test", tma(adminID), "")
	}
	if code, _ := test("pay.cryptobot"); code != 409 {
		t.Fatalf("a provider that is not active cannot be tested: %d", code)
	}
	if code, _ := test("general"); code != 400 {
		t.Fatalf("general has no check: %d", code)
	}
}

type fakeReceipts struct {
	gotID string
	sent  []int64
}

func (r *fakeReceipts) SendReceipt(_ context.Context, chatID int64, _ orders.ReceiptKind, id string) error {
	r.gotID = id
	r.sent = append(r.sent, chatID)
	return nil
}

func (r *fakeReceipts) Receipt(_ context.Context, id string) (io.ReadCloser, string, error) {
	r.gotID = id
	return io.NopCloser(strings.NewReader("JPEGDATA")), "application/octet-stream", nil
}

func TestAdminReceipt(t *testing.T) {
	d, f := adminApp(t)
	rc := &fakeReceipts{}
	d.Receipts = rc
	ctx := context.Background()
	c, _ := f.GetCustomer(ctx, 5)
	o, _ := d.Orders.Create(ctx, c, 77, orders.Request{Kind: orders.KindTopup, Amount: 10})
	if _, err := d.Orders.RequestReceipt(ctx, c.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Orders.AttachReceipt(ctx, 77, orders.ReceiptPhoto, "file-1", ""); err != nil {
		t.Fatal(err)
	}
	code, body := do(t, d, "GET", "/bot/api/admin/orders/"+o.ID+"/receipt", tma(adminID), "")
	if code != 200 || body != "JPEGDATA" || rc.gotID != "file-1" {
		t.Fatalf("receipt %d %q %q", code, body, rc.gotID)
	}
	if code, _ := do(t, d, "POST", "/bot/api/admin/orders/"+o.ID+"/receipt/send", tma(adminID), ""); code != 204 || len(rc.sent) != 1 || rc.sent[0] != adminID {
		t.Fatalf("send to the admin's chat: %d %v", code, rc.sent)
	}
	// A text receipt has no file.
	id := pending(t, d, f)
	if code, _ := do(t, d, "GET", "/bot/api/admin/orders/"+id+"/receipt", tma(adminID), ""); code != 404 {
		t.Fatalf("text receipt: %d", code)
	}
}

func TestReceiptHeaders(t *testing.T) {
	for _, c := range []struct {
		name   string
		kind   orders.ReceiptKind
		head   string
		want   string
		attach bool
	}{
		{"pdf", orders.ReceiptDocument, "%PDF-1.7\n%\xe2\xe3", "application/pdf", false},
		{"jpeg", orders.ReceiptDocument, "\xff\xd8\xff\xe0", "image/jpeg", false},
		{"png", orders.ReceiptDocument, "\x89PNG\r\n\x1a\n", "image/png", false},
		{"svg", orders.ReceiptDocument, "<?xml version=\"1.0\"?><svg xmlns=\"http://www.w3.org/2000/svg\"/>", "application/octet-stream", true},
		{"html", orders.ReceiptDocument, "<!DOCTYPE html><html>", "application/octet-stream", true},
		{"empty", orders.ReceiptDocument, "", "application/octet-stream", true},
		{"binary", orders.ReceiptDocument, "\x00\x01\x02\x03", "application/octet-stream", true},
		{"photo", orders.ReceiptPhoto, "<html>", "image/jpeg", false},
	} {
		if got, att := receiptHeaders(c.kind, []byte(c.head)); got != c.want || att != c.attach {
			t.Errorf("%s: %q %v", c.name, got, att)
		}
	}
}

func TestAdminSettingsTestRetired(t *testing.T) {
	d, _ := adminApp(t)
	body := `{"enabled":false,"token":"tok"}`
	if code, out := do(t, d, "PUT", "/bot/api/admin/settings/pay.cryptobot", tma(adminID), body); code != 200 {
		t.Fatalf("save %d %s", code, out)
	}
	d.Orders.UpdateLive(func(l *orders.Live) {
		l.Providers = payments.NewRegistry().Retire(&payments.CryptoBot{Token: "t"})
	})
	code, out := do(t, d, "POST", "/bot/api/admin/settings/pay.cryptobot/test", tma(adminID), "")
	if code != 409 || !strings.Contains(out, "not_active") {
		t.Fatalf("retired provider: %d %s", code, out)
	}
}

func TestAdminApproveBadDecisionKeepsAmount(t *testing.T) {
	d, f := adminApp(t)
	id := pending(t, d, f)
	if code, _ := do(t, d, "POST", "/bot/api/admin/orders/"+id+"/approve", tma(adminID), `{"decision":"x","amount":60}`); code != 400 {
		t.Fatalf("bad decision: %d", code)
	}
	o, _ := d.Orders.Get(context.Background(), id)
	if o.Received != nil {
		t.Fatalf("amount saved despite bad decision: %v", *o.Received)
	}
}

func TestAdminName(t *testing.T) {
	for want, u := range map[string]initdata.User{
		"@ann (5)":    {ID: 5, Username: "ann", FirstName: "Ann"},
		"Ann Lee (6)": {ID: 6, FirstName: "Ann", LastName: "Lee"},
		"7":           {ID: 7},
	} {
		if got := adminName(u); got != want {
			t.Errorf("%+v: %q, want %q", u, got, want)
		}
	}
}

type fakeProfile struct{ res settings.ProfileApply }

func (f *fakeProfile) ApplyProfile(context.Context) settings.ProfileApply { return f.res }
func (f *fakeProfile) ProfileDefaults() map[string]settings.ProfileTexts {
	return map[string]settings.ProfileTexts{"en": {Name: "Default"}}
}

func TestAdminProfileSaveReportsTelegram(t *testing.T) {
	d, _ := adminApp(t)
	put := func(body string) (int, string) {
		return do(t, d, "PUT", "/bot/api/admin/settings/profile", tma(adminID), body)
	}
	if code, body := put(`{"texts":{"ru":{"name":"` + strings.Repeat("a", 65) + `"}}}`); code != 422 {
		t.Fatalf("a name over Telegram's limit is refused: %d %s", code, body)
	}

	fp := &fakeProfile{res: settings.ProfileApply{
		Failed:     []settings.ProfileFail{{Lang: "ru", Field: "name", Message: "Too Many Requests"}},
		RetryAfter: 3600,
	}}
	d.Profile = fp

	code, body := put(`{"texts":{"ru":{"name":"Кабинет"}}}`)
	if code != 200 || !strings.Contains(body, `"retry_after":3600`) || !strings.Contains(body, "Кабинет") {
		t.Fatalf("save answers with what Telegram did: %d %s", code, body)
	}
	if got := d.Settings.Snapshot().Profile().Texts["ru"].Name; got != "Кабинет" {
		t.Fatalf("the text is saved even when Telegram refused it: %q", got)
	}
	if code, _ := put(`{"texts":{"ru":{"name":" Default ","about":"x"},"en":{"name":"Default"}}}`); code != 200 {
		t.Fatalf("save: %d", code)
	}
	if got := d.Settings.Snapshot().Profile().Texts; got["en"].Name != "" || got["ru"].About != "x" {
		t.Fatalf("a text equal to the built-in one is no override: %+v", got)
	}
	if _, body := do(t, d, "GET", "/bot/api/admin/settings", tma(adminID), ""); !strings.Contains(body, `"profile_defaults":{"en":{"name":"Default"`) {
		t.Fatalf("the form needs the built-in texts: %s", body)
	}
}
