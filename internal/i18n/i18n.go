// Package i18n holds the bot's text catalogs. Catalogs are flat JSON files
// (locales/<lang>.json) embedded into the binary; English is the base.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

//go:embed locales/*.json
var files embed.FS

// Base is the language every other one falls back to.
const Base = "en"

type Bundle struct{ cats map[string]map[string]string }

func Load() (*Bundle, error) {
	entries, err := files.ReadDir("locales")
	if err != nil {
		return nil, err
	}
	b := &Bundle{cats: map[string]map[string]string{}}
	for _, e := range entries {
		raw, err := files.ReadFile(path.Join("locales", e.Name()))
		if err != nil {
			return nil, err
		}
		cat := map[string]string{}
		if err := json.Unmarshal(raw, &cat); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		b.cats[strings.TrimSuffix(e.Name(), ".json")] = cat
	}
	if b.cats[Base] == nil {
		return nil, fmt.Errorf("i18n: locales/%s.json is missing", Base)
	}
	return b, nil
}

// Default is the bundle built from the embedded catalogs.
var Default = func() *Bundle {
	b, err := Load()
	if err != nil {
		panic(err)
	}
	return b
}()

func (b *Bundle) Langs() []string {
	out := make([]string, 0, len(b.cats))
	for l := range b.cats {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// Resolve maps a Telegram language_code ("ru-RU", "pt_BR") to a supported language, or Base.
func (b *Bundle) Resolve(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		code = code[:i]
	}
	if _, ok := b.cats[code]; ok {
		return code
	}
	return Base
}

func (b *Bundle) For(code string) Localizer { return Localizer{lang: b.Resolve(code), b: b} }

type Localizer struct {
	lang string
	b    *Bundle
}

func (l Localizer) bundle() *Bundle {
	if l.b == nil {
		return Default
	}
	return l.b
}

func (l Localizer) Lang() string {
	if l.lang == "" {
		return Base
	}
	return l.lang
}

func (l Localizer) lookup(key string) (string, bool) {
	b := l.bundle()
	if s, ok := b.cats[l.Lang()][key]; ok {
		return s, true
	}
	s, ok := b.cats[Base][key]
	return s, ok
}

func (l Localizer) Has(key string) bool { _, ok := l.lookup(key); return ok }

// Or returns the catalog entry for key as is, or fallback when the catalog has none (a name that needs no format).
func (l Localizer) Or(key, fallback string) string {
	if s, ok := l.lookup(key); ok {
		return s
	}
	return fallback
}

// T formats the catalog entry for key with fmt.Sprintf rules.
func (l Localizer) T(key string, args ...any) string {
	s, ok := l.lookup(key)
	if !ok {
		return key
	}
	if len(args) == 0 {
		return strings.ReplaceAll(s, "%%", "%")
	}
	return fmt.Sprintf(s, args...)
}

// Plural picks key_<category of n> (falling back to key_other) and formats it with args.
func (l Localizer) Plural(key string, n int, args ...any) string {
	if k := key + "_" + category(l.Lang(), n); l.Has(k) {
		return l.T(k, args...)
	}
	return l.T(key+"_other", args...)
}
