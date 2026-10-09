package telegram

import (
	"context"
	"testing"
)

func TestSetCommandsPerLanguage(t *testing.T) {
	b, ft, _ := newCardBot(t)
	ft.mu.Lock()
	ft.commands = nil
	ft.mu.Unlock()
	b.setCommands(context.Background())

	ft.mu.Lock()
	defer ft.mu.Unlock()
	byLang := map[string][]string{}
	for _, c := range ft.commands {
		byLang[c.LanguageCode] = c.Descriptions
	}
	if d := byLang[""]; len(d) != 3 || d[0] != "Main menu" {
		t.Errorf("default scope must be English, got %v", d)
	}
	if d := byLang["ru"]; len(d) != 3 || d[0] != "Главное меню" {
		t.Errorf("ru scope: %v", d)
	}
	if _, ok := byLang["en"]; ok {
		t.Error("English is the default scope and needs no separate language_code call")
	}
}
