// Package telegram connects go-telegram/bot updates to access + view.
package telegram

import (
	"context"
	"log/slog"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"vpn-control-bot/internal/access"
	"vpn-control-bot/internal/config"
	"vpn-control-bot/internal/notify"
	"vpn-control-bot/internal/orders"
	"vpn-control-bot/internal/profile"
	"vpn-control-bot/internal/refcode"
	"vpn-control-bot/internal/store"
	"vpn-control-bot/internal/telegram/view"
	"vpn-control-bot/internal/vpnctl"
)

// PromptStore keeps the admin questions awaiting a reply (*store.Store).
type PromptStore interface {
	SavePrompt(ctx context.Context, chatID int64, msgID int, p store.Prompt) error
	TakePrompt(ctx context.Context, chatID int64, msgID int) (store.Prompt, bool, error)
}

type Deps struct {
	Access    *access.Service
	API       vpnctl.Client
	Codec     *refcode.Codec
	WebAppURL string
	PayMode   config.PayMode
	Log       *slog.Logger

	Orders      *orders.Service // nil: payments in the bot are off
	Prompts     PromptStore
	AdminChatID int64
	IsAdmin     func(int64) bool
}

type Bot struct {
	Deps
	tg       *tgbot.Bot
	username string
	links    view.Links
}

func New(ctx context.Context, token string, d Deps) (*Bot, error) {
	b := &Bot{Deps: d, links: view.Links{WebApp: d.WebAppURL}}
	tg, err := tgbot.New(token,
		tgbot.WithDefaultHandler(b.onText),
		tgbot.WithMiddlewares(gate(d.AdminChatID)),
		tgbot.WithErrorsHandler(func(err error) { d.Log.Error("telegram", "err", err) }),
	)
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
	for _, p := range []string{"m:", view.CbSub, view.CbSubsPage, view.CbTariffsPage, view.CbRenew, view.CbOrdersPage} {
		tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, p, tgbot.MatchTypePrefix, b.onCallback)
	}
	for _, p := range []string{view.CbRenewPick, view.CbOrderPay, view.CbOrderBal, view.CbOrderCancel} {
		tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, p, tgbot.MatchTypePrefix, b.onOrderCallback)
	}
	tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, "o", tgbot.MatchTypePrefix, b.onAdmin)

	if _, err := tg.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: []models.BotCommand{
		{Command: "menu", Description: "Главное меню"},
		{Command: "start", Description: "Начать заново"},
		{Command: "help", Description: "Помощь"},
	}}); err != nil {
		d.Log.Warn("set commands", "err", err)
	}
	if _, err := tg.SetChatMenuButton(ctx, &tgbot.SetChatMenuButtonParams{MenuButton: models.MenuButtonWebApp{
		Type: models.MenuButtonTypeWebApp, Text: "Открыть", WebApp: models.WebAppInfo{URL: d.WebAppURL + "/"},
	}}); err != nil {
		d.Log.Warn("set menu button", "err", err)
	}
	return b, nil
}

// SendNotice sends a notification as a new message (notify.Runner's Send).
func (b *Bot) SendNotice(ctx context.Context, n notify.Notice) error {
	s := view.PaymentReminder(n, b.links, time.Now())
	if n.Kind == notify.Traffic {
		s = view.TrafficAlert(n, b.links, time.Now())
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
func (b *Bot) outcome(r access.Result, err error) (view.Screen, bool) {
	if err != nil {
		b.Log.Error("access", "err", err)
		return view.Plain(view.TxtDown), true
	}
	switch r.Outcome {
	case access.NeedLink:
		return view.Plain(view.TxtNeedLink), true
	case access.BadReferral:
		return view.Plain(view.TxtBadReferral), true
	case access.Archived:
		return view.Plain(view.TxtArchived), true
	case access.BadLink:
		return view.Plain(view.TxtBadLink), true
	case access.NotFound:
		return view.Plain(view.TxtNotFound), true
	case access.Taken:
		return view.Plain(view.TxtTaken), true
	}
	return view.Screen{}, false
}

func (b *Bot) profile(c *vpnctl.Customer) profile.Profile {
	return profile.Build(c, RefLink(b.username, b.Codec, c.ID), b.PayMode)
}

func (b *Bot) home(r access.Result, prefix string) view.Screen {
	s := view.Home(b.profile(r.Customer), b.links, time.Now())
	if prefix != "" {
		s.Text = prefix + "\n\n" + s.Text
	}
	return s
}

func (b *Bot) onStart(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	if m.Chat.Type != models.ChatTypePrivate {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(m.Text, "/start"))
	r, err := b.Access.Start(ctx, user(m.From), payload)
	if s, done := b.outcome(r, err); done {
		b.show(ctx, m.Chat.ID, 0, s)
		return
	}
	prefix := ""
	if r.Outcome == access.Created {
		prefix = view.TxtWelcome
	}
	b.show(ctx, m.Chat.ID, 0, b.home(r, prefix))
}

func (b *Bot) onMenu(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	if m.Chat.Type != models.ChatTypePrivate {
		return
	}
	r, err := b.Access.Start(ctx, user(m.From), "")
	if s, done := b.outcome(r, err); done {
		b.show(ctx, m.Chat.ID, 0, s)
		return
	}
	b.show(ctx, m.Chat.ID, 0, b.home(r, ""))
}

func (b *Bot) onHelp(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	if u.Message.Chat.Type != models.ChatTypePrivate {
		return
	}
	b.show(ctx, u.Message.Chat.ID, 0, view.Notice(view.TxtHelp))
}

// onText handles any other message: a subscription link while we wait for
// one, otherwise it just brings the menu back.
func (b *Bot) onText(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m := u.Message
	if m == nil {
		return
	}
	if m.Chat.Type != models.ChatTypePrivate {
		b.onAdminReply(ctx, m) // the gate lets only admin-chat replies through
		return
	}
	// A private admin chat may answer a prompt; anything else is a customer's.
	if b.AdminChatID != 0 && m.Chat.ID == b.AdminChatID && m.ReplyToMessage != nil && b.onAdminReply(ctx, m) {
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
		if s, done := b.outcome(r, err); done {
			b.show(ctx, m.Chat.ID, 0, s)
			return
		}
		b.show(ctx, m.Chat.ID, 0, b.home(r, view.TxtLinked))
		return
	}
	b.onMenu(ctx, b.tg, u)
}

func (b *Bot) onCallback(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	q := u.CallbackQuery
	answer := &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID}
	defer func() { b.tg.AnswerCallbackQuery(ctx, answer) }()

	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	rt, ok := parseRoute(q.Data)
	if !ok {
		return
	}
	r, err := b.Access.Start(ctx, user(&q.From), "")
	if s, done := b.outcome(r, err); done {
		b.show(ctx, chatID, msgID, s)
		return
	}
	p := b.profile(r.Customer)
	now := time.Now()
	switch rt.kind {
	case view.CbHome:
		b.show(ctx, chatID, msgID, view.Home(p, b.links, now))
	case view.CbSubs, view.CbSubsPage:
		b.show(ctx, chatID, msgID, view.Subscriptions(p, rt.id, now))
	case view.CbSub:
		s, found := view.Subscription(p, rt.id, b.links, now)
		if !found {
			s = view.Subscriptions(p, 0, now)
		}
		b.show(ctx, chatID, msgID, s)
	case view.CbBalance:
		b.show(ctx, chatID, msgID, view.Balance(p, b.links))
	case view.CbRefs:
		b.show(ctx, chatID, msgID, view.Referrals(p, b.links))
	case view.CbTariffs, view.CbTariffsPage:
		ts, err := b.API.ListTariffs(ctx)
		if err != nil {
			b.Log.Error("tariffs", "err", err)
			b.show(ctx, chatID, msgID, view.Notice(view.TxtDown))
			return
		}
		b.show(ctx, chatID, msgID, view.Tariffs(profile.Tariffs(ts), rt.id, b.links))
	case view.CbRenew:
		if b.Orders == nil {
			answer.Text, answer.ShowAlert = view.TxtSoon, true
			return
		}
		b.renewPeriods(ctx, chatID, msgID, r.Customer, rt.id)
	case view.CbOrders, view.CbOrdersPage:
		if b.Orders == nil {
			answer.Text, answer.ShowAlert = view.TxtSoon, true
			return
		}
		b.showOrders(ctx, chatID, msgID, r.Customer.ID, rt.id)
	}
}
