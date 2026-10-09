package view

import (
	"strconv"
	"unicode/utf8"

	"vpn-control-bot/internal/i18n"
)

const (
	subsPerPage    = 8
	tariffsPerPage = 5
	maxAddons      = 10  // add-ons listed on a subscription card
	maxTitle       = 60  // characters of a title in text and buttons
	maxDescription = 300 // characters of a tariff description
)

// page clamps n to [0, pages) and returns the slice bounds for it.
func page(n, total, size int) (cur, pages, from, to int) {
	pages = max(1, (total+size-1)/size)
	cur = min(max(n, 0), pages-1)
	from = cur * size
	return cur, pages, from, min(from+size, total)
}

// pager is the "‹ Back · Next ›" row; nil when everything fits one page.
func pager(loc i18n.Localizer, prefix string, cur, pages int) []btn {
	if pages <= 1 {
		return nil
	}
	var row []btn
	if cur > 0 {
		row = append(row, cb(loc.T("btn.prev"), prefix+strconv.Itoa(cur-1)))
	}
	if cur < pages-1 {
		row = append(row, cb(loc.T("btn.next"), prefix+strconv.Itoa(cur+1)))
	}
	return row
}

func pageTitle(loc i18n.Localizer, cur, pages int) string {
	if pages <= 1 {
		return ""
	}
	return loc.T("fmt.page_of", cur+1, pages)
}

// clip shortens s to n characters, ending with "…".
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
