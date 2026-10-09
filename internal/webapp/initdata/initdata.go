// Package initdata validates Telegram WebApp initData
// (https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app).
package initdata

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid = errors.New("initdata: invalid")
	ErrExpired = errors.New("initdata: expired")
)

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	// LanguageCode is Telegram's IETF tag for the user's language ("" when not reported).
	LanguageCode string `json:"language_code"`
}

func Validate(raw, botToken string, maxAge time.Duration, now time.Time) (User, error) {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return User{}, ErrInvalid
	}
	hash := v.Get("hash")
	if hash == "" || !hmac.Equal([]byte(hash), []byte(sum(v, botToken))) {
		return User{}, ErrInvalid
	}
	auth, err := strconv.ParseInt(v.Get("auth_date"), 10, 64)
	if err != nil {
		return User{}, ErrInvalid
	}
	if now.Sub(time.Unix(auth, 0)) > maxAge {
		return User{}, ErrExpired
	}
	var u User
	if err := json.Unmarshal([]byte(v.Get("user")), &u); err != nil || u.ID == 0 {
		return User{}, ErrInvalid
	}
	return u, nil
}

// Sign returns v encoded with a valid hash for botToken.
func Sign(v url.Values, botToken string) string {
	v.Del("hash")
	v.Set("hash", sum(v, botToken))
	return v.Encode()
}

// sum is hex(HMAC_SHA256(HMAC_SHA256("WebAppData", token), data_check_string)).
func sum(v url.Values, botToken string) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + v.Get(k)
	}
	secret := mac([]byte("WebAppData"), []byte(botToken))
	return hex.EncodeToString(mac(secret, []byte(strings.Join(lines, "\n"))))
}

func mac(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}
