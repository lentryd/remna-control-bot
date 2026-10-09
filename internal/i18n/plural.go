package i18n

// category returns the CLDR plural category used as a key suffix for n in lang.
// Unknown languages use the English rule. Add a case here when a language needs more forms.
func category(lang string, n int) string {
	if n < 0 {
		n = -n
	}
	switch lang {
	case "ru", "uk":
		switch {
		case n%10 == 1 && n%100 != 11:
			return "one"
		case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
			return "few"
		default:
			return "many"
		}
	default:
		if n == 1 {
			return "one"
		}
		return "other"
	}
}
