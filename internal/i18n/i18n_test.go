package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func fixture() *Bundle {
	return &Bundle{cats: map[string]map[string]string{
		"en": {"hi": "Hello, %s", "only_en": "English only", "day_one": "%d day", "day_other": "%d days"},
		"ru": {"hi": "Привет, %s", "day_one": "%d день", "day_few": "%d дня", "day_many": "%d дней", "day_other": "%d дня"},
	}}
}

func TestResolve(t *testing.T) {
	b := fixture()
	for in, want := range map[string]string{
		"": "en", "xx": "en", "pt-BR": "en", "en": "en", "en-US": "en",
		"ru": "ru", "ru-RU": "ru", "ru_UA": "ru", "RU": "ru", "  ru ": "ru",
	} {
		if got := b.Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTFallbacks(t *testing.T) {
	b := fixture()
	if got := b.For("ru").T("hi", "Аня"); got != "Привет, Аня" {
		t.Errorf("got %q", got)
	}
	if got := b.For("ru").T("only_en"); got != "English only" {
		t.Errorf("missing ru key must fall back to en, got %q", got)
	}
	if got := b.For("xx").T("hi", "Ann"); got != "Hello, Ann" {
		t.Errorf("unknown language must be en, got %q", got)
	}
	if got := b.For("ru").T("nope"); got != "nope" {
		t.Errorf("missing everywhere must return the key, got %q", got)
	}
}

func TestZeroLocalizerIsEnglish(t *testing.T) {
	var l Localizer
	if l.Lang() != "en" {
		t.Fatalf("zero Localizer lang = %q", l.Lang())
	}
}

func TestPlural(t *testing.T) {
	b := fixture()
	ru := b.For("ru")
	for n, want := range map[int]string{
		0: "0 дней", 1: "1 день", 2: "2 дня", 4: "4 дня", 5: "5 дней",
		11: "11 дней", 12: "12 дней", 14: "14 дней", 21: "21 день", 22: "22 дня", 101: "101 день", 111: "111 дней",
	} {
		if got := ru.Plural("day", n, n); got != want {
			t.Errorf("ru %d: got %q, want %q", n, got, want)
		}
	}
	en := b.For("en")
	for n, want := range map[int]string{0: "0 days", 1: "1 day", 2: "2 days", 21: "21 days"} {
		if got := en.Plural("day", n, n); got != want {
			t.Errorf("en %d: got %q, want %q", n, got, want)
		}
	}
}

var verb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

func verbs(s string) string {
	v := verb.FindAllString(strings.ReplaceAll(s, "%%", ""), -1)
	sort.Strings(v)
	return strings.Join(v, ",")
}

func baseKey(k string) string {
	for _, suf := range []string{"_zero", "_one", "_two", "_few", "_many", "_other"} {
		if strings.HasSuffix(k, suf) {
			return strings.TrimSuffix(k, suf)
		}
	}
	return k
}

// TestCatalogParity: every shipped language has the base keys of en and the same format verbs.
// Plural forms are compared too: every form of every language must use the verbs of en's base key.
func TestCatalogParity(t *testing.T) {
	b := Default
	en := b.cats[Base]
	enKeys := map[string]string{} // base key → verbs of its plain / _other form
	for k, v := range en {
		if k == baseKey(k) || strings.HasSuffix(k, "_other") {
			enKeys[baseKey(k)] = verbs(v)
		}
	}
	for _, lang := range b.Langs() {
		got := map[string]bool{}
		for k, v := range b.cats[lang] {
			base := baseKey(k)
			got[base] = true
			want, ok := enKeys[base]
			if !ok {
				t.Errorf("%s: extra key %q not in en", lang, k)
			} else if g := verbs(v); g != want {
				t.Errorf("%s: key %q verbs %q, en has %q", lang, k, g, want)
			}
		}
		for k := range enKeys {
			if !got[k] {
				t.Errorf("%s: missing key %q", lang, k)
			}
		}
	}
}

func TestOr(t *testing.T) {
	ru := fixture().For("ru")
	for key, want := range map[string]string{"hi": "Привет, %s", "only_en": "English only", "nope": "own"} {
		if got := ru.Or(key, "own"); got != want {
			t.Errorf("Or(%q) = %q, want %q", key, got, want)
		}
	}
}
