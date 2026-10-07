package payments

import (
	"slices"
	"testing"
)

func TestRegistry(t *testing.T) {
	a, b := &Fake{Name: "a"}, &Fake{Name: "b"}
	r := NewRegistry(a, b)
	if got := r.IDs(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("ids %v", got)
	}
	if p, ok := r.Get("b"); !ok || p != Provider(b) {
		t.Fatalf("get b: %v %v", p, ok)
	}
	if _, ok := r.Get("zzz"); ok {
		t.Fatal("unknown provider found")
	}
	if len(r.Enabled()) != 2 || r.Title("a") != "Fake pay" || r.Title("zzz") != "" {
		t.Fatalf("enabled %d title %q", len(r.Enabled()), r.Title("a"))
	}
	var none *Registry
	if len(none.Enabled()) != 0 || len(none.IDs()) != 0 || none.Title("a") != "" {
		t.Fatal("nil registry must be empty")
	}
	if _, ok := none.Get("a"); ok {
		t.Fatal("nil registry found a provider")
	}
}

func TestClipAndMoney(t *testing.T) {
	if clip("абвгд", 3) != "абв" || clip("ab", 3) != "ab" {
		t.Error("clip is rune-based")
	}
	if money(1500) != "1500.00" || money(0.1+0.2) != "0.30" {
		t.Errorf("money %q %q", money(1500), money(0.1+0.2))
	}
}
