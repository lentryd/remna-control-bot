package telegram

import (
	"testing"

	"vpn-control-bot/internal/refcode"
)

func TestParseRoute(t *testing.T) {
	cases := map[string]route{
		"m:home": {kind: "m:home"}, "m:subs": {kind: "m:subs"}, "m:bal": {kind: "m:bal"},
		"m:ref": {kind: "m:ref"}, "m:tar": {kind: "m:tar"},
		"s:12": {kind: "s:", id: 12},
		"ps:0": {kind: "ps:", id: 0}, "ps:2": {kind: "ps:", id: 2}, "pt:1": {kind: "pt:", id: 1}, "r:3": {kind: "r:", id: 3},
		"m:pay": {kind: "m:pay"}, "po:0": {kind: "po:", id: 0}, "po:4": {kind: "po:", id: 4},
	}
	for in, want := range cases {
		got, ok := parseRoute(in)
		if !ok || got != want {
			t.Errorf("parseRoute(%q) = %+v,%v", in, got, ok)
		}
	}
	for _, in := range []string{"", "x", "s:", "s:abc", "s:-1", "s:0", "t:3", "ps:-1", "pt:x", "m:nope", "po:-1", "po:x"} {
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

func TestParseIDs(t *testing.T) {
	if got, ok := parseIDs("rp:3:1:0", "rp:", 3); !ok || got[0] != 3 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("%v %v", got, ok)
	}
	for _, in := range []string{"rp:3:1", "rp:3:1:0:0", "rp:a:1:0", "rp:3:-1:0", "pr:3:1:0"} {
		if _, ok := parseIDs(in, "rp:", 3); ok {
			t.Errorf("%q accepted", in)
		}
	}
}
