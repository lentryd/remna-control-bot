package telegram

import (
	"testing"

	"vpn-control-bot/internal/refcode"
)

func TestParseRoute(t *testing.T) {
	got, ok := parseRoute("m:home")
	if !ok || got != (route{kind: "m:home"}) {
		t.Fatalf("parseRoute(m:home) = %+v,%v", got, ok)
	}
	// callbacks of screens that moved to the WebApp are not routes any more
	for _, in := range []string{"", "x", "m:subs", "m:bal", "m:ref", "m:tar", "m:pay", "s:12", "ps:0", "pt:1", "r:3", "po:0", "rp:3:1:0", "pr:x", "pb:x", "pp:a:b"} {
		if _, ok := parseRoute(in); ok {
			t.Errorf("parseRoute(%q) accepted", in)
		}
	}
}

func TestParseOrderID(t *testing.T) {
	const id = "0c6f2b3a-5d4e-4f1a-9b7c-8e2d1a3f4b5c"
	if got, ok := parseOrderID("pc:"+id, "pc:"); !ok || got != id {
		t.Fatalf("%q %v", got, ok)
	}
	for _, in := range []string{"pc:", "pc:1", "pc:" + id + "x", "pr:" + id} {
		if _, ok := parseOrderID(in, "pc:"); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestRefLink(t *testing.T) {
	c := refcode.New("s")
	if got := RefLink("vpnbot", c, 5); got != "https://t.me/vpnbot?start=ref_"+c.Encode(5) {
		t.Fatal(got)
	}
}
