package telegram

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/i18n"
)

func first(s sentForm, key string) string {
	if v := s.Values[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// byLangOf maps language_code -> the value of key over every call to method.
func byLangOf(ft *fakeTelegram, method, key string) map[string]string {
	out := map[string]string{}
	for _, s := range ft.sent(method) {
		out[first(s, "language_code")] = first(s, key)
	}
	return out
}

func TestInitProfileFillsEverything(t *testing.T) {
	b, ft, _ := newCardBot(t)
	if err := b.initProfile(context.Background()); err != nil {
		t.Fatal(err)
	}

	names := byLangOf(ft, "setMyName", "name")
	if names[""] != "Remna Control" || names["ru"] == "" {
		t.Errorf("names: %v", names)
	}
	for _, method := range []string{"setMyDescription", "setMyShortDescription"} {
		key := map[string]string{"setMyDescription": "description", "setMyShortDescription": "short_description"}[method]
		got := byLangOf(ft, method, key)
		for _, lang := range []string{"", "ru"} {
			if !strings.Contains(got[lang], RepoURL) {
				t.Errorf("%s[%q] must link the repository: %q", method, lang, got[lang])
			}
		}
	}

	photos := ft.sent("setMyProfilePhoto")
	if len(photos) != 1 || len(photos[0].Files) != 1 || !strings.Contains(first(photos[0], "photo"), `"static"`) {
		t.Fatalf("one static photo must be uploaded: %+v", photos)
	}
	if n := len(ft.sent("setMyDefaultAdministratorRights")); n != 2 {
		t.Errorf("rights for groups and channels: %d calls", n)
	}
	if ft.count("setChatMenuButton") < 2 {
		t.Error("the menu button is set again")
	}
}

func TestProfileTextsFitTelegramLimits(t *testing.T) {
	for _, l := range i18n.Default.Langs() {
		loc := i18n.Default.For(l)
		for key, max := range map[string]int{"profile.name": 64, "profile.description": 512, "profile.about": 120} {
			text := loc.T(key)
			if key != "profile.name" {
				text = loc.T(key, RepoURL)
			}
			if n := utf8.RuneCountInString(text); n > max {
				t.Errorf("%s/%s: %d characters, the limit is %d", l, key, n, max)
			}
		}
	}
}

// TestRussianProfileIsNeutral: the profile is seen before anyone starts the bot, so the Russian one names no VPN.
func TestRussianProfileIsNeutral(t *testing.T) {
	loc := i18n.Default.For("ru")
	for _, key := range []string{"profile.name", "profile.about", "profile.description"} {
		if text := loc.T(key, RepoURL); strings.Contains(strings.ToLower(strings.ReplaceAll(text, RepoURL, "")), "vpn") {
			t.Errorf("ru/%s mentions VPN: %q", key, text)
		}
	}
}

func TestInitCommandOnlyInAdminChats(t *testing.T) {
	b, ft, _ := newCardBot(t)
	ft.mu.Lock()
	ft.commands = nil
	ft.mu.Unlock()
	if err := b.setCommands(context.Background()); err != nil {
		t.Fatal(err)
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()
	admins := map[string]bool{}
	for _, c := range ft.commands {
		hasInit := len(c.Descriptions) == 5
		if c.Scope == "" && hasInit {
			t.Errorf("the default scope must not offer /init: %v", c.Descriptions)
		}
		if c.Scope != "" {
			if !hasInit {
				t.Errorf("an admin's scope must offer /admin and /init: %+v", c)
			}
			admins[c.Scope+" "+c.LanguageCode] = true
		}
	}
	if len(admins) != 3*len(i18n.Default.Langs()) {
		t.Errorf("every admin in every language: %v", admins)
	}
}

func initUpdate(from int64) *models.Update {
	return &models.Update{Message: &models.Message{
		Text: "/init", From: &models.User{ID: from}, Chat: models.Chat{ID: from, Type: models.ChatTypePrivate},
	}}
}

func TestInitCommandIgnoresNonAdmins(t *testing.T) {
	b, ft, _ := newCardBot(t)
	b.onInitCmd(context.Background(), nil, initUpdate(42))
	if ft.count("setMyName") != 0 || ft.count("sendMessage") != 0 {
		t.Errorf("a stranger's /init must do nothing: %v", ft.calls)
	}
}

func TestInitCommandReportsToAdmin(t *testing.T) {
	b, ft, _ := newCardBot(t)
	ctx := i18n.NewContext(context.Background(), i18n.Default.For("en"))
	b.onInitCmd(ctx, nil, initUpdate(1))
	if ft.count("setMyName") == 0 || ft.count("setMyProfilePhoto") != 1 {
		t.Errorf("/init from an admin fills the profile: %v", ft.calls)
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if txt := ft.texts[1]; len(txt) != 1 || !strings.Contains(txt[0], "profile") {
		t.Errorf("the admin gets a report: %q", txt)
	}
}

func TestFirstStartFillsBlankProfile(t *testing.T) {
	_, ft, _ := newBotWith(t, &fakeTelegram{blank: true})
	if ft.count("setMyName") == 0 || ft.count("setMyProfilePhoto") != 1 {
		t.Errorf("a blank profile is filled on start: %v", ft.calls)
	}

	_, ft, _ = newCardBot(t)
	if ft.count("setMyName") != 0 || ft.count("setMyProfilePhoto") != 0 {
		t.Errorf("a filled profile is left alone: %v", ft.calls)
	}
}
