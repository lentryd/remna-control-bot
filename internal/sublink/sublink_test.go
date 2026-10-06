package sublink

import "testing"

func TestParse(t *testing.T) {
	ok := map[string]string{
		"https://sub.example.com/AbC123xyz":           "AbC123xyz",
		"  https://sub.example.com/AbC123xyz/  ":      "AbC123xyz",
		"https://sub.example.com/AbC123xyz/?a=1#frag": "AbC123xyz",
		"https://example.com/sub/path/Zz_-09abc":      "Zz_-09abc",
		"http://1.2.3.4:2096/AbC123xyz":               "AbC123xyz",
		"AbC123xyz":                                   "AbC123xyz",
	}
	for in, want := range ok {
		got, k := Parse(in)
		if !k || got != want {
			t.Errorf("Parse(%q) = %q,%v want %q", in, got, k, want)
		}
	}
	for _, in := range []string{"", "hello there", "abc", "https://example.com/", "ftp://x.com/AbC123xyz", "https://x.com/a b", "<b>AbC123</b>"} {
		if got, k := Parse(in); k {
			t.Errorf("Parse(%q) = %q, want reject", in, got)
		}
	}
}
