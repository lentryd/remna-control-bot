package settings

import (
	"fmt"
	"regexp"
	"unicode/utf8"
)

// Telegram's limits on the bot profile texts, in characters.
const (
	MaxProfileName        = 64
	MaxProfileDescription = 512
	MaxProfileAbout       = 120
)

var langRe = regexp.MustCompile(`^[a-z]{2}$`)

// ProfileTexts overrides the bot's profile texts for one language; an empty field keeps the built-in text.
type ProfileTexts struct {
	Name        string `json:"name"`
	Description string `json:"description"` // shown in an empty chat with the bot
	About       string `json:"about"`       // the short description of the profile page
}

// Profile is what admins changed in the bot's profile, by language code ("en" is the default scope).
type Profile struct {
	Texts map[string]ProfileTexts `json:"texts"`
}

func (Profile) Key() string         { return KeyProfile }
func (*Profile) Secrets() []*string { return nil }

func (p Profile) Validate() error {
	for lang, t := range p.Texts {
		if !langRe.MatchString(lang) {
			return fmt.Errorf("texts: %q is not a two-letter language code", lang)
		}
		for _, f := range []struct {
			name, v string
			max     int
		}{{"name", t.Name, MaxProfileName}, {"description", t.Description, MaxProfileDescription}, {"about", t.About, MaxProfileAbout}} {
			if n := utf8.RuneCountInString(f.v); n > f.max {
				return fmt.Errorf("texts.%s.%s is %d characters, Telegram allows %d", lang, f.name, n, f.max)
			}
		}
	}
	return nil
}

// ProfileFail is one text Telegram refused.
type ProfileFail struct {
	Lang    string `json:"lang"`
	Field   string `json:"field"` // name | description | about
	Message string `json:"message"`
}

// ProfileApply reports what pushing the saved profile to Telegram did. A text that failed is still saved; it is
// tried again with the next save.
type ProfileApply struct {
	Failed     []ProfileFail `json:"failed"`
	RetryAfter int           `json:"retry_after"` // seconds Telegram asked to wait; 0 when it did not
}
