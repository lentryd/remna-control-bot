package telegram

import (
	"context"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// allowed lets through private chats with a known sender and, in the admin
// chat, only admin-card buttons and replies to the bot's prompts: in any
// group the customer screens would post one member's data for everyone.
func allowed(u *models.Update, adminChat int64) bool {
	switch {
	case u.Message != nil:
		m := u.Message
		if m.From == nil {
			return false
		}
		if m.Chat.Type == models.ChatTypePrivate {
			return true
		}
		return adminChat != 0 && m.Chat.ID == adminChat && m.ReplyToMessage != nil && !isCommand(m)
	case u.CallbackQuery != nil:
		m := u.CallbackQuery.Message.Message
		if m == nil {
			return false
		}
		if m.Chat.Type == models.ChatTypePrivate {
			return true
		}
		return adminChat != 0 && m.Chat.ID == adminChat && strings.HasPrefix(u.CallbackQuery.Data, "o")
	}
	return false
}

func gate(adminChat int64) tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
			if allowed(u, adminChat) {
				next(ctx, b, u)
			}
		}
	}
}

// isCommand: the message starts with a bot command, which must never run in a group.
func isCommand(m *models.Message) bool {
	if strings.HasPrefix(m.Text, "/") {
		return true
	}
	for _, e := range m.Entities {
		if e.Type == models.MessageEntityTypeBotCommand && e.Offset == 0 {
			return true
		}
	}
	return false
}
