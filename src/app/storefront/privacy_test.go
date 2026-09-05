package storefront

import (
	"strings"
	"testing"
)

// The page has no switch: it is owed whether or not the owner opened the settings.
func TestPrivacyAlwaysServed(t *testing.T) {
	_, h := setup(t)

	body := get(t, h, "/privacy")
	if !strings.Contains(body, "персональных данных") {
		t.Fatal("the policy text is missing from the page")
	}
	if !strings.Contains(body, "Лавка") {
		t.Fatal("the policy does not name the shop it belongs to")
	}

	if !strings.Contains(get(t, h, "/sitemap.xml"), "/privacy") {
		t.Fatal("the page is not in the sitemap")
	}
	if !strings.Contains(get(t, h, "/cart"), `href="/privacy"`) {
		t.Fatal("the footer does not link to the policy")
	}
}
