package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"vpn-control-bot/internal/i18n"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/vpnctl"
)

// fakeTelegram answers the Bot API (<base>/bot<token>/<method>, multipart form): chat 2 is an admin who never
// started the bot.
type fakeTelegram struct {
	mu    sync.Mutex
	calls []string // "method chat_id"

	texts    map[int64][]string // chat -> text of each sendMessage
	commands []setCmd           // every setMyCommands call

	editFail map[int64]string // chat -> "code description" for editMessageText
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	r.ParseMultipartForm(1 << 20)
	chat, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
	f.mu.Lock()
	f.calls = append(f.calls, method+" "+strconv.FormatInt(chat, 10))
	if method == "sendMessage" {
		if f.texts == nil {
			f.texts = map[int64][]string{}
		}
		f.texts[chat] = append(f.texts[chat], r.FormValue("text")+"\n"+r.FormValue("reply_markup"))
	}
	if method == "setMyCommands" {
		var cmds []struct {
			Description string `json:"description"`
		}
		json.Unmarshal([]byte(r.FormValue("commands")), &cmds)
		c := setCmd{LanguageCode: r.FormValue("language_code")}
		for _, x := range cmds {
			c.Descriptions = append(c.Descriptions, x.Description)
		}
		f.commands = append(f.commands, c)
	}
	f.mu.Unlock()
	f.mu.Lock()
	fail, failing := f.editFail[chat]
	f.mu.Unlock()
	switch {
	case method == "editMessageText" && failing:
		code, desc, _ := strings.Cut(fail, " ")
		n, _ := strconv.Atoi(code)
		w.WriteHeader(n)
		io.WriteString(w, `{"ok":false,"error_code":`+code+`,"description":"`+desc+`"}`)
	case method == "getMe":
		io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"b","username":"vpnbot"}}`)
	case (method == "sendMessage" || method == "editMessageText") && chat == 2:
		w.WriteHeader(403)
		io.WriteString(w, `{"ok":false,"error_code":403,"description":"Forbidden: bot can't initiate conversation with a user"}`)
	case method == "sendMessage" || method == "editMessageText":
		io.WriteString(w, `{"ok":true,"result":{"message_id":77,"date":1,"chat":{"id":1,"type":"private"}}}`)
	default:
		io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

type setCmd struct {
	LanguageCode string
	Descriptions []string
}

type memLangs map[int64]string

func (m memLangs) SetLang(_ context.Context, id int64, l string) error { m[id] = l; return nil }
func (m memLangs) Lang(_ context.Context, id int64) (string, error)    { return m[id], nil }

func (f *fakeTelegram) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func newCardBot(t *testing.T) (*Bot, *fakeTelegram, *store.Store) {
	t.Helper()
	ft := &fakeTelegram{}
	srv := httptest.NewServer(ft)
	t.Cleanup(srv.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b, err := New(context.Background(), "123:ABC", Deps{
		API: vpnctl.NewFake(), WebAppURL: "https://x/bot", Admins: []int64{1, 2, 3}, Notices: st, APIBase: srv.URL,
		IsAdmin: func(id int64) bool { return id >= 1 && id <= 3 },
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b, ft, st
}

func TestCardFanOut(t *testing.T) {
	b, ft, st := newCardBot(t)
	ctx := context.Background()
	o := orders.Order{ID: "11111111-2222", CustomerID: 5, ChatID: 77, Title: "t", Price: 10, ToPay: 10, Status: orders.StatusPendingReview}

	err := b.Card(ctx, o)
	if err == nil || !strings.Contains(err.Error(), "admin 2") {
		t.Fatalf("one admin is unreachable: the error must say who: %v", err)
	}
	if got := ft.count("sendMessage"); got != 3 {
		t.Fatalf("every admin is tried, even after one fails: %d sends", got)
	}
	notices, _ := st.Notices(ctx, o.ID)
	if len(notices) != 2 || notices[1] != 77 || notices[3] != 77 || notices[2] != 0 {
		t.Fatalf("only delivered cards are remembered: %v", notices)
	}
	if u := b.Unreachable(); len(u) != 1 || u[0] != 2 {
		t.Fatalf("unreachable: %v", u)
	}

	// A decision edits the cards that exist; it does not send new ones.
	o.Status, o.Admin = orders.StatusApproved, "@boss"
	b.Card(ctx, o)
	if sends, edits := ft.count("sendMessage"), ft.count("editMessageText"); sends != 3 || edits != 2 {
		t.Fatalf("after the decision: %d sends, %d edits", sends, edits)
	}
}

func TestCardSkipsOrdersNobodyHasToDecide(t *testing.T) {
	b, ft, _ := newCardBot(t)
	o := orders.Order{ID: "balance-1", Status: orders.StatusApproved} // paid from the balance: no card was ever sent
	if err := b.Card(context.Background(), o); err != nil || ft.count("sendMessage") != 0 {
		t.Fatalf("approved order without cards must stay silent: %v, %d sends", err, ft.count("sendMessage"))
	}
	o.Status = orders.StatusFailed
	b.Card(context.Background(), o)
	if ft.count("sendMessage") != 3 {
		t.Fatalf("a failed order needs the admins: %d sends", ft.count("sendMessage"))
	}
}

func TestCardEditGoneSendsFreshCard(t *testing.T) {
	b, ft, st := newCardBot(t)
	ctx := context.Background()
	o := orders.Order{ID: "22222222-3333", CustomerID: 5, ChatID: 77, Title: "t", Price: 10, ToPay: 10, Status: orders.StatusFailed}
	st.SaveNotice(ctx, o.ID, 1, 41)
	ft.editFail = map[int64]string{1: "400 Bad Request: message to edit not found"}

	if err := b.Card(ctx, o); err != nil && !strings.Contains(err.Error(), "admin 2") {
		t.Fatal(err)
	}
	if ft.count("editMessageText 1") != 1 || ft.count("sendMessage 1") != 1 {
		t.Fatalf("a vanished card must be re-sent: %v", ft.calls)
	}
	if n, _ := st.Notices(ctx, o.ID); n[1] != 77 {
		t.Fatalf("the notice must point at the new message: %v", n)
	}

	// Resolved order: nothing to re-send, and no error either.
	o2 := orders.Order{ID: "33333333-4444", Status: orders.StatusApproved}
	st.SaveNotice(ctx, o2.ID, 1, 42)
	before := ft.count("sendMessage")
	if err := b.Card(ctx, o2); err != nil || ft.count("sendMessage") != before {
		t.Fatalf("resolved order with a vanished card must be ignored: %v", err)
	}
}

func TestCardNon403FailureKeepsAdminReachable(t *testing.T) {
	b, ft, st := newCardBot(t)
	ctx := context.Background()
	o := orders.Order{ID: "44444444-5555", CustomerID: 5, ChatID: 77, Title: "t", Price: 10, ToPay: 10, Status: orders.StatusApproved}
	st.SaveNotice(ctx, o.ID, 1, 41)
	ft.editFail = map[int64]string{1: "502 Bad Gateway"}
	if err := b.Card(ctx, o); err == nil {
		t.Fatal("the failure must still be reported")
	}
	if u := b.Unreachable(); len(u) != 0 {
		t.Fatalf("a 5xx is not a reason to mark the admin unreachable: %v", u)
	}
}

func TestCardIsLocalizedPerAdmin(t *testing.T) {
	b, ft, _ := newCardBot(t)
	b.Langs = memLangs{1: "ru"} // admin 3 has no stored language
	o := orders.Order{ID: "55555555-6666", CustomerID: 5, ChatID: 77, Title: "t", Price: 10, ToPay: 10, Status: orders.StatusPendingReview}
	b.Card(context.Background(), o)
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ru, en := ft.texts[1], ft.texts[3]
	if len(ru) != 1 || len(en) != 1 {
		t.Fatalf("each admin gets one card: %v", ft.texts)
	}
	if !strings.Contains(ru[0], "Ждёт решения") || strings.Contains(ru[0], "Awaiting a decision") {
		t.Errorf("admin 1 must get a Russian card: %q", ru[0])
	}
	if !strings.Contains(en[0], "Awaiting a decision") || strings.Contains(en[0], "Ждёт решения") {
		t.Errorf("admin 3 must get an English card: %q", en[0])
	}
}

func TestNotifyAdminsLocalizedButton(t *testing.T) {
	b, ft, _ := newCardBot(t)
	b.Langs = memLangs{1: "ru"}
	b.NotifyAdmins(context.Background(), i18n.M("admin.no_methods"), "/admin/methods")
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ru, en := i18n.Default.For("ru"), i18n.Default.For("")
	if got := ft.texts[1][0]; !strings.Contains(got, ru.T("btn.open_admin")) || !strings.Contains(got, "Способы оплаты не настроены") {
		t.Errorf("admin 1 text and button must be Russian: %q", got)
	}
	if got := ft.texts[3][0]; !strings.Contains(got, en.T("btn.open_admin")) || !strings.Contains(got, "Payment methods aren’t set up") {
		t.Errorf("admin 3 text and button must be English: %q", got)
	}
}

func TestAlertIsLocalizedPerAdmin(t *testing.T) {
	b, ft, _ := newCardBot(t)
	b.Langs = memLangs{1: "ru"} // admin 3 has no stored language
	err := b.Alert(context.Background(), orders.Alert{Key: "alert.unmatched_payment", Args: []any{"pal24", "ord-1", "inv-9", "123.45", "RUB"}})
	if err == nil || !strings.Contains(err.Error(), "admin 2") {
		t.Errorf("admin 2 never started the bot: %v", err)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ru, en := ft.texts[1], ft.texts[3]
	if len(ru) != 1 || !strings.Contains(ru[0], "Платёж из сервиса pal24 не удалось сопоставить") || !strings.Contains(ru[0], "123.45 RUB") {
		t.Errorf("admin 1 must get a Russian alert: %q", ru)
	}
	if len(en) != 1 || !strings.Contains(en[0], "A payment from pal24 couldn’t be matched") || !strings.Contains(en[0], "inv-9") {
		t.Errorf("admin 3 must get an English alert: %q", en)
	}
}
