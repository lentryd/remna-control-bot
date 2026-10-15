// Package telegram connects go-telegram/bot updates to access + view.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remna-control-bot/internal/access"
	"remna-control-bot/internal/config"
	"remna-control-bot/internal/i18n"
	"remna-control-bot/internal/notify"
	"remna-control-bot/internal/orders"
	"remna-control-bot/internal/profile"
	"remna-control-bot/internal/refcode"
	"remna-control-bot/internal/remnactl"
	"remna-control-bot/internal/telegram/view"
)

func (d Deps) payMode() config.PayMode {
	if d.PayMode == nil {
		return config.PayMixed
	}
	return d.PayMode()
}

type Deps struct {
	Access    *access.Service
	API       remnactl.Client
	Codec     *refcode.Codec
	WebAppURL string
	PayMode   func() config.PayMode
	Log       *slog.Logger

	Orders  *orders.Service // nil: payments in the bot are off
	IsAdmin func(int64) bool
	Admins  []int64     // receive order cards; ADMIN_IDS in order
	Notices NoticeStore // where each admin's card for an order lives
	Langs   LangStore   // nil: everything is English
	APIBase string      // Bot API base URL; "" is Telegram's own (tests point it at a fake)
}

type Bot struct {
	Deps
	tg       *tgbot.Bot
	username string
	links    view.Links

	mu          sync.Mutex
	unreachable map[int64]bool // admins the last message to failed for
}

func New(ctx context.Context, token string, d Deps) (*Bot, error) {
	b := &Bot{Deps: d, links: view.Links{WebApp: d.WebAppURL}}
	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(b.onUpdate),
		tgbot.WithMiddlewares(gate(), b.langMiddleware()),
		tgbot.WithErrorsHandler(func(err error) { d.Log.Error("telegram", "err", err) }),
	}
	if d.APIBase != "" {
		opts = append(opts, tgbot.WithServerURL(d.APIBase))
	}
	tg, err := tgbot.New(token, opts...)
	if err != nil {
		return nil, err
	}
	b.tg = tg
	me, err := tg.GetMe(ctx)
	if err != nil {
		return nil, err
	}
	b.username = me.Username

	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "start", tgbot.MatchTypeCommand, b.onStart)
	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "menu", tgbot.MatchTypeCommand, b.onMenu)
	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "help", tgbot.MatchTypeCommand, b.onHelp)
	tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, view.CbHome, tgbot.MatchTypeExact, b.onCallback)
	tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, view.CbOrderCancel, tgbot.MatchTypePrefix, b.onOrderCallback)
	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "admin", tgbot.MatchTypeCommand, b.onAdminCmd)
	tg.RegisterHandler(tgbot.HandlerTypeMessageText, "init", tgbot.MatchTypeCommand, b.onInitCmd)

	if err := b.setCommands(ctx); err != nil {
		d.Log.Warn("set commands", "err", err)
	}
	if err := b.setMenuButton(ctx); err != nil {
		d.Log.Warn("set menu button", "err", err)
	}
	b.initIfBlank(ctx)
	return b, nil
}

// setCommands registers the command menu: English as the default scope, every other language under its own
// code. Each admin's chat gets the same menu plus /admin and /init.
func (b *Bot) setCommands(ctx context.Context) error {
	return perLang(func(code string, loc i18n.Localizer) error {
		public := []models.BotCommand{
			{Command: "menu", Description: loc.T("cmd.menu")},
			{Command: "start", Description: loc.T("cmd.start")},
			{Command: "help", Description: loc.T("cmd.help")},
		}
		errs := []error{b.setCommandList(ctx, code, nil, public)}
		admin := append(slices.Clip(public),
			models.BotCommand{Command: "admin", Description: loc.T("cmd.admin")},
			models.BotCommand{Command: "init", Description: loc.T("cmd.init")},
		)
		for _, id := range b.Admins {
			errs = append(errs, b.setCommandList(ctx, code, &models.BotCommandScopeChat{ChatID: id}, admin))
		}
		return errors.Join(errs...)
	})
}

func (b *Bot) setCommandList(ctx context.Context, code string, scope models.BotCommandScope, cmds []models.BotCommand) error {
	_, err := b.tg.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{LanguageCode: code, Scope: scope, Commands: cmds})
	if err != nil {
		return fmt.Errorf("lang %q scope %v: %w", code, scope, err)
	}
	return nil
}

// SendNotice sends a notification as a new message (notify.Runner's Send).
func (b *Bot) SendNotice(ctx context.Context, n notify.Notice) error {
	loc := b.locFor(ctx, n.ChatID)
	s := view.PaymentReminder(loc, n, b.links, time.Now())
	if n.Kind == notify.Traffic {
		s = view.TrafficAlert(loc, n, b.links, time.Now())
	}
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: n.ChatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: s.Keyboard,
	})
	return err
}

func (b *Bot) Username() string        { return b.username }
func (b *Bot) Run(ctx context.Context) { b.tg.Start(ctx) }

func user(u *models.User) access.User {
	return access.User{ID: u.ID, Name: strings.TrimSpace(u.FirstName + " " + u.LastName)}
}

// show edits msgID in place, or sends a new message when msgID is 0.
func (b *Bot) show(ctx context.Context, chatID int64, msgID int, s view.Screen) {
	var markup models.ReplyMarkup
	if s.Keyboard != nil {
		markup = s.Keyboard
	}
	if msgID != 0 {
		_, err := b.tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{
			ChatID: chatID, MessageID: msgID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup,
		})
		if err == nil || strings.Contains(err.Error(), "message is not modified") {
			return
		}
		b.Log.Warn("edit message, sending new", "err", err)
	}
	if _, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID, Text: s.Text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup,
	}); err != nil {
		b.Log.Error("send message", "err", err)
	}
}

// outcome turns a non-member access result into a screen. ok=false means
// the user is a member and the caller should render the requested page.
func (b *Bot) outcome(loc i18n.Localizer, r access.Result, err error) (view.Screen, bool) {
	if err != nil {
		b.Log.Error("access", "err", err)
		return view.Plain(loc, "msg.down"), true
	}
	switch r.Outcome {
	case access.NeedLink:
		return view.Plain(loc, "msg.need_link"), true
	case access.BadReferral:
		return view.Plain(loc, "msg.bad_referral"), true
	case access.Archived:
		return view.Plain(loc, "msg.archived"), true
	case access.BadLink:
		return view.Plain(loc, "msg.bad_link"), true
	case access.NotFound:
		return view.Plain(loc, "msg.not_found"), true
	case access.Taken:
		return view.Plain(loc, "msg.taken"), true
	}
	return view.Screen{}, false
}

func (b *Bot) profile(c *remnactl.Customer) profile.Profile {
	return profile.Build(c, RefLink(b.username, b.Codec, c.ID), b.payMode())
}

func (b *Bot) home(loc i18n.Localizer, r access.Result, prefix string) view.Screen {
	s := view.Home(loc, b.profile(r.Customer), b.links, time.Now())
	if prefix != "" {
		s.Text = prefix + "\n\n" + s.Text
	}
	return s
}

func (b *Bot) onStart(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	m := u.Message
	if m.Chat.Type != models.ChatTypePrivate {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(m.Text, "/start"))
	r, err := b.Access.Start(ctx, user(m.From), payload)
	if s, done := b.outcome(loc, r, err); done {
		b.show(ctx, m.Chat.ID, 0, s)
		return
	}
	prefix := ""
	if r.Outcome == access.Created {
		prefix = loc.T("msg.welcome")
	}
	b.show(ctx, m.Chat.ID, 0, b.home(loc, r, prefix))
}

func (b *Bot) onMenu(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	m := u.Message
	if m.Chat.Type != models.ChatTypePrivate {
		return
	}
	r, err := b.Access.Start(ctx, user(m.From), "")
	if s, done := b.outcome(loc, r, err); done {
		b.show(ctx, m.Chat.ID, 0, s)
		return
	}
	b.show(ctx, m.Chat.ID, 0, b.home(loc, r, ""))
}

func (b *Bot) onHelp(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	if u.Message.Chat.Type != models.ChatTypePrivate {
		return
	}
	b.show(ctx, u.Message.Chat.ID, 0, view.Notice(loc, loc.T("msg.help")))
}

// onUpdate routes what no handler matched: Stars' payment events, else text.
func (b *Bot) onUpdate(ctx context.Context, tb *tgbot.Bot, u *models.Update) {
	switch {
	case u.PreCheckoutQuery != nil:
		b.onPreCheckout(ctx, u.PreCheckoutQuery)
	case u.Message != nil && u.Message.SuccessfulPayment != nil:
		b.onStarsPaid(ctx, u.Message)
	default:
		b.onText(ctx, tb, u)
	}
}

// onText handles any other message: a subscription link while we wait for
// one, otherwise it just brings the menu back.
func (b *Bot) onText(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	m := u.Message
	if m == nil {
		return
	}
	if m.Chat.Type != models.ChatTypePrivate {
		return
	}
	if b.onReceipt(ctx, m) {
		return
	}
	who := user(m.From)
	waiting, err := b.Access.AwaitingLink(ctx, who.ID)
	if err != nil {
		b.Log.Error("state", "err", err)
	}
	if waiting {
		r, err := b.Access.Link(ctx, who, m.Text)
		if s, done := b.outcome(loc, r, err); done {
			b.show(ctx, m.Chat.ID, 0, s)
			return
		}
		b.show(ctx, m.Chat.ID, 0, b.home(loc, r, loc.T("msg.linked")))
		return
	}
	b.onMenu(ctx, b.tg, u)
}

func (b *Bot) onCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	loc := b.loc(ctx)
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { _, _ = b.tg.AnswerCallbackQuery(ctx, answer) }()

	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	if _, ok := parseRoute(q.Data); !ok {
		return
	}
	r, err := b.Access.Start(ctx, user(&q.From), "")
	if s, done := b.outcome(loc, r, err); done {
		b.show(ctx, chatID, msgID, s)
		return
	}
	b.show(ctx, chatID, msgID, view.Home(loc, b.profile(r.Customer), b.links, time.Now()))
}
