package initdata

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

func signed(authDate time.Time) string {
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(authDate.Unix(), 10))
	v.Set("query_id", "AAH")
	v.Set("user", `{"id":42,"first_name":"Ann","username":"ann"}`)
	return Sign(v, "123:ABC")
}

func TestValid(t *testing.T) {
	u, err := Validate(signed(now.Add(-time.Minute)), "123:ABC", 24*time.Hour, now)
	if err != nil || u.ID != 42 || u.FirstName != "Ann" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
}

func TestRejects(t *testing.T) {
	good := signed(now.Add(-time.Minute))
	cases := map[string]struct {
		raw, token string
		want       error
	}{
		"other token": {good, "999:XYZ", ErrInvalid},
		"tampered":    {strings.Replace(good, "Ann", "Bob", 1), "123:ABC", ErrInvalid},
		"no hash":     {"auth_date=1&user=%7B%7D", "123:ABC", ErrInvalid},
		"garbage":     {"%%%", "123:ABC", ErrInvalid},
		"expired":     {signed(now.Add(-25 * time.Hour)), "123:ABC", ErrExpired},
	}
	for name, c := range cases {
		if _, err := Validate(c.raw, c.token, 24*time.Hour, now); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestLanguageCode(t *testing.T) {
	for name, tc := range map[string]struct{ user, want string }{
		"ru":     {`{"id":1,"first_name":"A","language_code":"ru"}`, "ru"},
		"absent": {`{"id":1,"first_name":"A"}`, ""},
	} {
		v := url.Values{}
		v.Set("auth_date", strconv.FormatInt(now.Add(-time.Minute).Unix(), 10))
		v.Set("user", tc.user)
		u, err := Validate(Sign(v, "123:ABC"), "123:ABC", 24*time.Hour, now)
		if err != nil || u.LanguageCode != tc.want {
			t.Errorf("%s: u=%+v err=%v", name, u, err)
		}
	}
}
