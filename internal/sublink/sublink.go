// Package sublink extracts a Remnawave short uuid from what a user pastes:
// a subscription URL (the uuid is its last path segment) or the bare uuid.
package sublink

import (
	"net/url"
	"regexp"
	"strings"
)

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

func Parse(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if tokenRe.MatchString(s) {
		return s, true
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if last := segs[len(segs)-1]; tokenRe.MatchString(last) {
		return last, true
	}
	return "", false
}
