package telegram

import (
	"testing"

	"vpn-control-bot/internal/refcode"
)

func TestParseRoute(t *testing.T) {
	cases := map[string]route{
		"m:home": {kind: "m:home"}, "m:subs": {kind: "m:subs"}, "m:bal": {kind: "m:bal"},
		"m:ref": {kind: "m:ref"}, "m:tar": {kind: "m:tar"},
		"s:12": {kind: "s:", id: 12}, "t:3": {kind: "t:", id: 3},
		"ps:0": {kind: "ps:", id: 0}, "ps:2": {kind: "ps:", id: 2}, "pt:1": {kind: "pt:", id: 1},
	}
	for in, want := range cases {
		got, ok := parseRoute(in)
		if !ok || got != want {
			t.Errorf("parseRoute(%q) = %+v,%v", in, got, ok)
		}
	}
	for _, in := range []string{"", "x", "s:", "s:abc", "s:-1", "s:0", "ps:-1", "pt:x", "m:nope"} {
		if _, ok := parseRoute(in); ok {
			t.Errorf("parseRoute(%q) accepted", in)
		}
	}
}

func TestRefLink(t *testing.T) {
	c := refcode.New("s")
	if got := RefLink("vpnbot", c, 5); got != "https://t.me/vpnbot?start=ref_"+c.Encode(5) {
		t.Fatal(got)
	}
}
