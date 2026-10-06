// Package view renders bot screens (HTML text + inline keyboard) from data.
// It is pure: no network, so screens are covered by golden tests.
package view

import "github.com/go-telegram/bot/models"

type Screen struct {
	Text     string
	Keyboard *models.InlineKeyboardMarkup
}

type Links struct {
	WebApp string // PUBLIC_URL + WEBAPP_PATH
}

// Page opens a WebApp section, e.g. Page("/subscriptions/3").
func (l Links) Page(hash string) *models.WebAppInfo {
	return &models.WebAppInfo{URL: l.WebApp + "/#" + hash}
}

const (
	CbHome    = "m:home"
	CbSubs    = "m:subs"
	CbBalance = "m:bal"
	CbRefs    = "m:ref"
	CbTariffs = "m:tar"
	CbSub     = "s:" // + subscription id
	CbTariff  = "t:" // + tariff id

	CbSubsPage    = "ps:" // + page number, from 0
	CbTariffsPage = "pt:" // + page number, from 0
)

type btn = models.InlineKeyboardButton

func kb(rows ...[]btn) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func cb(text, data string) btn { return btn{Text: text, CallbackData: data} }

func back(to string) []btn { return []btn{cb(BtnBack, to)} }
