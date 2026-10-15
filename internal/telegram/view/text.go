package view

import "unicode/utf8"

// maxTitle is the number of characters of a title in text and buttons.
const maxTitle = 60

// clip shortens s to n characters, ending with "…".
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
