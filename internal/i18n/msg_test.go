package i18n

import (
	"context"
	"testing"
)

func TestMsgStoredAndRenderedPerLanguage(t *testing.T) {
	b := &Bundle{cats: map[string]map[string]string{
		"en": {"buy": "Renewal of “%s” · %s", "mo": "%d mo.", "sum": "%s paid %.2f"},
		"ru": {"buy": "Продление «%s» · %s", "mo": "%d мес."},
	}}
	stored := M("buy", "Дом <b>", M("mo", 3)).String()
	if got := b.For("en").Text(stored); got != "Renewal of “Дом <b>” · 3 mo." {
		t.Errorf("en: %q", got)
	}
	if got := b.For("ru").Text(stored); got != "Продление «Дом <b>» · 3 мес." {
		t.Errorf("ru: %q", got)
	}
	if got := b.For("ru").Text(M("sum", "Ann", 12.5).String()); got != "Ann paid 12.50" {
		t.Errorf("floats survive storage and missing keys fall back to en: %q", got)
	}
	if got := b.For("ru").Render(M("buy", "x", M("mo", 1))); got != "Продление «x» · 1 мес." {
		t.Errorf("render: %q", got)
	}
}

func TestTextKeepsPlainStrings(t *testing.T) {
	ru := Default.For("ru")
	for _, s := range []string{"", "Продление «Дом» · 3 мес.", "no free squad", "i18n:{broken", "i18n:42"} {
		if got := ru.Text(s); got != s {
			t.Errorf("%q became %q", s, got)
		}
	}
}

func TestContextLocalizer(t *testing.T) {
	ctx := context.Background()
	if FromContext(ctx).Lang() != Base {
		t.Error("no localizer in the context means English")
	}
	if FromContext(NewContext(ctx, Default.For("ru"))).Lang() != "ru" {
		t.Error("the stored localizer comes back")
	}
}
