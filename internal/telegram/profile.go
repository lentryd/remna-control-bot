package telegram

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/settings"
)

// RepoURL is where the bot's source lives; its profile links to it.
const RepoURL = "https://github.com/lentryd/remna-control-bot"

// avatar is the bot's profile photo; assets/avatar.svg is its source.
//
//go:embed assets/avatar.jpg
var avatar []byte

// perLang calls set for the default scope (English) and every other language under its own code.
func perLang(set func(code string, loc i18n.Localizer) error) error {
	errs := []error{set("", i18n.Default.For(i18n.Base))}
	for _, l := range i18n.Default.Langs() {
		if l != i18n.Base {
			errs = append(errs, set(l, i18n.Default.For(l)))
		}
	}
	return errors.Join(errs...)
}

// profileFields in the order they are applied: the name goes last, because Telegram limits how often it changes.
var profileFields = []string{"description", "about", "name"}

// profileLangs is every language the profile is set in, the default scope (English) first.
func profileLangs() []string {
	langs := []string{i18n.Base}
	for _, l := range i18n.Default.Langs() {
		if l != i18n.Base {
			langs = append(langs, l)
		}
	}
	return langs
}

// langCode is the language_code the Bot API wants: "" is the default scope.
func langCode(lang string) string {
	if lang == i18n.Base {
		return ""
	}
	return lang
}

func textField(t *settings.ProfileTexts, field string) *string {
	switch field {
	case "name":
		return &t.Name
	case "description":
		return &t.Description
	}
	return &t.About
}

// profileTexts is what the profile says in lang: the admin's text, else the built-in one.
func (b *Bot) profileTexts(lang string) settings.ProfileTexts {
	var over settings.ProfileTexts
	if b.Profile != nil {
		over = b.Profile().Texts[lang]
	}
	def := builtinProfile(lang)
	pick := func(custom, builtin string) string {
		if strings.TrimSpace(custom) != "" {
			return custom
		}
		return builtin
	}
	return settings.ProfileTexts{
		Name:        pick(over.Name, def.Name),
		Description: pick(over.Description, def.Description),
		About:       pick(over.About, def.About),
	}
}

func builtinProfile(lang string) settings.ProfileTexts {
	loc := i18n.Default.For(lang)
	return settings.ProfileTexts{
		Name:        loc.T("profile.name"),
		Description: loc.T("profile.description", RepoURL),
		About:       loc.T("profile.about", RepoURL),
	}
}

// ProfileDefaults is the built-in profile text of every language, which a saved empty text falls back to.
func (b *Bot) ProfileDefaults() map[string]settings.ProfileTexts {
	out := map[string]settings.ProfileTexts{}
	for _, l := range profileLangs() {
		out[l] = builtinProfile(l)
	}
	return out
}

// setText sends one profile text to Telegram and remembers it as what the profile now says.
func (b *Bot) setText(ctx context.Context, lang, field, value string) error {
	var err error
	switch field {
	case "name":
		_, err = b.tg.SetMyName(ctx, &tgbot.SetMyNameParams{Name: value, LanguageCode: langCode(lang)})
	case "description":
		_, err = b.tg.SetMyDescription(ctx, &tgbot.SetMyDescriptionParams{Description: value, LanguageCode: langCode(lang)})
	default:
		_, err = b.tg.SetMyShortDescription(ctx, &tgbot.SetMyShortDescriptionParams{ShortDescription: value, LanguageCode: langCode(lang)})
	}
	if err != nil {
		return err
	}
	b.mu.Lock()
	t := b.applied[lang]
	*textField(&t, field) = value
	b.applied[lang] = t
	b.mu.Unlock()
	return nil
}

// syncApplied assumes the profile in Telegram says what the settings say now (it was set that way on an earlier
// run), so a save that changes nothing sends nothing.
func (b *Bot) syncApplied() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.applied = map[string]settings.ProfileTexts{}
	for _, l := range profileLangs() {
		b.applied[l] = b.profileTexts(l)
	}
}

// ApplyProfile sends the saved profile texts that differ from what Telegram has. Changing a bot's name is
// rate-limited hard (a 429 can ask to wait for hours), so only changed texts are sent, a 429 stops that kind of
// text for the rest of the call, and nothing is retried: a text that failed is tried again by the next save.
func (b *Bot) ApplyProfile(ctx context.Context) settings.ProfileApply {
	b.applyMu.Lock()
	defer b.applyMu.Unlock()
	res := settings.ProfileApply{Failed: []settings.ProfileFail{}}
	blocked := map[string]bool{}
	for _, field := range profileFields {
		for _, lang := range profileLangs() {
			want := b.profileTexts(lang)
			b.mu.Lock()
			have := b.applied[lang]
			b.mu.Unlock()
			value := *textField(&want, field)
			if value == *textField(&have, field) {
				continue
			}
			if blocked[field] {
				res.Failed = append(res.Failed, settings.ProfileFail{Lang: lang, Field: field, Message: "skipped: Telegram asked to wait"})
				continue
			}
			if err := b.setText(ctx, lang, field, value); err != nil {
				var tooMany *tgbot.TooManyRequestsError
				if errors.As(err, &tooMany) {
					blocked[field] = true
					res.RetryAfter = max(res.RetryAfter, tooMany.RetryAfter)
				}
				b.Log.Warn("apply bot profile", "lang", lang, "field", field, "err", err)
				res.Failed = append(res.Failed, settings.ProfileFail{Lang: lang, Field: field, Message: err.Error()})
			}
		}
	}
	return res
}

// initProfile fills everything about the bot that the Bot API can set: name, descriptions, commands, menu
// button, default rights and photo. Each step is tried even if an earlier one failed; the joined error names
// the steps that did.
func (b *Bot) initProfile(ctx context.Context) error {
	step := func(name string, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
	texts := func(field string) error {
		var errs []error
		for _, l := range profileLangs() {
			want := b.profileTexts(l)
			if err := b.setText(ctx, l, field, *textField(&want, field)); err != nil {
				errs = append(errs, fmt.Errorf("lang %q: %w", langCode(l), err))
			}
		}
		return errors.Join(errs...)
	}
	return errors.Join(
		step("name", texts("name")),
		step("description", texts("description")),
		step("short description", texts("about")),
		step("commands", b.setCommands(ctx)),
		step("menu button", b.setMenuButton(ctx)),
		// The bot works in private chats only, so it asks for no rights when added to a group or channel.
		step("rights", errors.Join(b.setNoRights(ctx, false), b.setNoRights(ctx, true))),
		step("photo", b.setPhoto(ctx)),
	)
}

func (b *Bot) setNoRights(ctx context.Context, channels bool) error {
	_, err := b.tg.SetMyDefaultAdministratorRights(ctx, &tgbot.SetMyDefaultAdministratorRightsParams{
		Rights: &models.ChatAdministratorRights{}, ForChannels: channels,
	})
	return err
}

func (b *Bot) setPhoto(ctx context.Context) error {
	_, err := b.tg.SetMyProfilePhoto(ctx, &tgbot.SetMyProfilePhotoParams{Photo: &models.InputProfilePhotoStatic{
		Photo: "attach://avatar.jpg", MediaAttachment: bytes.NewReader(avatar),
	}})
	return err
}

func (b *Bot) setMenuButton(ctx context.Context) error {
	_, err := b.tg.SetChatMenuButton(ctx, &tgbot.SetChatMenuButtonParams{MenuButton: models.MenuButtonWebApp{
		Type: models.MenuButtonTypeWebApp, Text: i18n.Default.For(i18n.Base).T("menu.open"), WebApp: models.WebAppInfo{URL: b.WebAppURL + "/"},
	}})
	return err
}

// descriptionPause is how long to wait before asking for the description again; the answer is occasionally empty
// or cut off on a start, when the network behind the container is not up yet.
var descriptionPause = 2 * time.Second

// readDescription asks for the bot's description, trying three times before giving up.
func (b *Bot) readDescription(ctx context.Context) (models.BotDescription, error) {
	var err error
	for i := range 3 {
		if i > 0 {
			select {
			case <-ctx.Done():
				return models.BotDescription{}, ctx.Err()
			case <-time.After(descriptionPause):
			}
		}
		var d models.BotDescription
		if d, err = b.tg.GetMyDescription(ctx, &tgbot.GetMyDescriptionParams{}); err == nil {
			return d, nil
		}
	}
	return models.BotDescription{}, err
}

// initIfBlank fills the profile on the first start: a bot whose description is empty was never set up, here or
// in @BotFather. A profile someone already filled is left alone; /init overwrites it on purpose.
func (b *Bot) initIfBlank(ctx context.Context) {
	d, err := b.readDescription(ctx)
	if err != nil {
		b.Log.Warn("read bot description", "err", err)
		return
	}
	if d.Description != "" {
		return
	}
	b.Log.Info("blank bot profile, filling it")
	if err := b.initProfile(ctx); err != nil {
		b.Log.Warn("fill bot profile", "err", err)
	}
}

// onInitCmd: /init fills the bot's profile again, for an admin, and reports how it went.
func (b *Bot) onInitCmd(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	if m.Chat.Type != models.ChatTypePrivate || !b.isAdmin(m.From.ID) {
		return
	}
	loc := b.loc(ctx)
	text := loc.T("msg.init_done")
	if err := b.initProfile(ctx); err != nil {
		b.Log.Warn("fill bot profile", "err", err)
		text = loc.T("msg.init_failed", strings.ReplaceAll(err.Error(), "\n", "\n• "))
	}
	if _, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: m.Chat.ID, Text: text}); err != nil {
		b.Log.Error("init command", "err", err)
	}
}
