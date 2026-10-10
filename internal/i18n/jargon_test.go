package i18n

import (
	"strings"
	"testing"
)

// TestNoJargon keeps internal jargon out of every catalog.
func TestNoJargon(t *testing.T) {
	banned := map[string][]string{
		"ru": {"админк", "админ ", "админы", "провайдер", "аддон", "квитанци", "администратор", "панел"},
		"en": {"admin panel", "adminka", "provider", "addon", "add-on"},
	}
	for lang, words := range banned {
		for k, v := range Default.cats[lang] {
			low := strings.ToLower(v)
			if lang == "ru" {
				// "VPN-панель" is the product's own name for the panel; a bare "панель" is jargon.
				low = strings.ReplaceAll(low, "vpn-панел", "")
			}
			for _, w := range words {
				if strings.Contains(low, w) {
					t.Errorf("%s/%s contains jargon %q: %q", lang, k, w, v)
				}
			}
		}
	}
}
