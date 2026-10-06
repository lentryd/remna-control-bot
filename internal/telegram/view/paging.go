package view

import (
	"fmt"
	"strconv"
	"unicode/utf8"
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

// pager is the "‹ Назад · Далее ›" row; nil when everything fits one page.
func pager(prefix string, cur, pages int) []btn {
	if pages <= 1 {
		return nil
	}
	var row []btn
	if cur > 0 {
		row = append(row, cb(BtnPrev, prefix+strconv.Itoa(cur-1)))
	}
	if cur < pages-1 {
		row = append(row, cb(BtnNext, prefix+strconv.Itoa(cur+1)))
	}
	return row
}

func pageTitle(cur, pages int) string {
	if pages <= 1 {
		return ""
	}
	return fmt.Sprintf(" · %d из %d", cur+1, pages)
}

// clip shortens s to n characters, ending with "…".
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
