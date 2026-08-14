package mail

import (
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
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

// The sender is not the login: a relay authenticates by an API key, and a
// Workspace alias signs in as the real mailbox — in both cases the letter must
// come from the shop's own address, not from the credentials.
func TestSenderFallsBackToLogin(t *testing.T) {
	s := &database.Settings{SMTPUser: "apikey"}
	if got := sender(s); got != "apikey" {
		t.Errorf("no explicit sender: got %q", got)
	}
	s.SMTPFrom = "shop@example.com"
	if got := sender(s); got != "shop@example.com" {
		t.Errorf("explicit sender ignored: got %q", got)
	}
	msg := string(buildMessage(sender(s), "owner@example.com", "Тема", "тело"))
	if !strings.Contains(msg, "From: shop@example.com\r\n") {
		t.Errorf("From header: %q", msg)
	}
}

// The inbox should show the shop, not the mailbox: "info" tells the owner
// nothing about which shop just sold something.
func TestFromHeader(t *testing.T) {
	cases := []struct{ name, addr, want string }{
		{"", "shop@x.by", "shop@x.by"},
		{"Лавка Ивана", "shop@x.by", "=?UTF-8?b?0JvQsNCy0LrQsCDQmNCy0LDQvdCw?= <shop@x.by>"},
		{`Ivan's Shop, Ltd`, "shop@x.by", `"Ivan's Shop, Ltd" <shop@x.by>`},
		{`Say "hi"`, "shop@x.by", `"Say \"hi\"" <shop@x.by>`},
	}
	for _, c := range cases {
		if got := fromHeader(c.name, c.addr); got != c.want {
			t.Errorf("fromHeader(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
