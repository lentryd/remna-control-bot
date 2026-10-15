package telegram

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/i18n"
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
	return errors.Join(
		step("name", perLang(func(code string, loc i18n.Localizer) error {
			_, err := b.tg.SetMyName(ctx, &tgbot.SetMyNameParams{Name: loc.T("profile.name"), LanguageCode: code})
			return err
		})),
		step("description", perLang(func(code string, loc i18n.Localizer) error {
			_, err := b.tg.SetMyDescription(ctx, &tgbot.SetMyDescriptionParams{
				Description: loc.T("profile.description", RepoURL), LanguageCode: code,
			})
			return err
		})),
		step("short description", perLang(func(code string, loc i18n.Localizer) error {
			_, err := b.tg.SetMyShortDescription(ctx, &tgbot.SetMyShortDescriptionParams{
				ShortDescription: loc.T("profile.about", RepoURL), LanguageCode: code,
			})
			return err
		})),
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

// initIfBlank fills the profile on the first start: a bot whose description is empty was never set up, here or
// in @BotFather. A profile someone already filled is left alone; /init overwrites it on purpose.
func (b *Bot) initIfBlank(ctx context.Context) {
	d, err := b.tg.GetMyDescription(ctx, &tgbot.GetMyDescriptionParams{})
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
