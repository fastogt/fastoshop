package mail

import (
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	msg := buildMessage("shop@x.ru", "owner@x.ru", "Новый заказ #7", "Иван, +7999…")
	s := string(msg)
	for _, want := range []string{
		"From: shop@x.ru", "To: owner@x.ru",
		"Subject: =?UTF-8?b?", // subject is encoded — Cyrillic breaks otherwise
		"Content-Type: text/plain; charset=UTF-8",
		"Иван",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q:\n%s", want, s)
		}
	}
}
