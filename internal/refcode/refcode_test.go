package refcode

import (
	"regexp"
	"testing"
)

var codeRe = regexp.MustCompile(`^[0-9A-Za-z]{5,10}$`)

func TestRoundTripUniqueShort(t *testing.T) {
	c := New("secret")
	seen := map[string]int{}
	for _, id := range append(seq(1, 5000), 1<<20, 1<<31-1) {
		code := c.Encode(id)
		if !codeRe.MatchString(code) {
			t.Fatalf("id %d: bad code %q", id, code)
		}
		if prev, dup := seen[code]; dup {
			t.Fatalf("ids %d and %d share code %q", prev, id, code)
		}
		seen[code] = id
		got, ok := c.Decode(code)
		if !ok || got != id {
			t.Fatalf("Decode(%q) = %d,%v want %d", code, got, ok, id)
		}
	}
}

func TestTamperAndForeignSecret(t *testing.T) {
	c := New("secret")
	code := c.Encode(42)
	last := code[len(code)-1]
	repl := byte('a')
	if last == 'a' {
		repl = 'b'
	}
	if _, ok := c.Decode(code[:len(code)-1] + string(repl)); ok {
		t.Error("tampered signature accepted")
	}
	if _, ok := New("other").Decode(code); ok {
		t.Error("code from another secret accepted")
	}
}

func TestGarbage(t *testing.T) {
	c := New("secret")
	for _, s := range []string{"", "abcd", "!!!!!!", "ref_abc", "zzzzzzzzzzzzzzzzzzzzzz", "0" + c.Encode(7)} {
		if _, ok := c.Decode(s); ok {
			t.Errorf("Decode(%q) accepted", s)
		}
	}
}

func seq(a, b int) []int {
	out := make([]int, 0, b-a+1)
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}
