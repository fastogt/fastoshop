package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestTranslations(t *testing.T) {
	if got := T(LangEN, KeyNegativePrice); got != "price cannot be negative" {
		t.Errorf("en: %q", got)
	}
	// An unknown language is not a reason to lose the message.
	if T("de", KeyNegativePrice) != T(LangRU, KeyNegativePrice) {
		t.Error("unknown language must fall back to the default")
	}
	// Every key must exist in both languages, or one of them silently ships empty.
	for key, m := range kMessages {
		if m[0] == "" || m[1] == "" {
			t.Errorf("key %q is missing a translation: %q", key, m)
		}
	}
}

// Our sentinels get translated; text from the platform is passed through untouched.
func TestTIfKeyLeavesForeignTextAlone(t *testing.T) {
	if got := TIfKey(LangEN, KeyOzonNoAnswer); got != "Ozon did not answer for this article" {
		t.Errorf("own key not translated: %q", got)
	}
	const platform = "Товар не найден в системе"
	if got := TIfKey(LangEN, platform); got != platform {
		t.Errorf("platform text must survive: %q", got)
	}
}

var kVerb = regexp.MustCompile(`%[a-z]`)

// A missing form or a verb in one language only ships a broken sentence to the buyer.
func TestPagePhrases(t *testing.T) {
	for key, m := range kPage {
		if m[0] == "" || m[1] == "" {
			t.Errorf("key %q is missing a translation: %q", key, m)
			continue
		}
		ru, en := strings.Split(m[0], "|"), strings.Split(m[1], "|")
		if len(ru) > 1 || len(en) > 1 {
			if len(ru) != 3 || len(en) != 2 {
				t.Errorf("plural %q needs 3 ru forms and 2 en ones: %q", key, m)
			}
		}
		if a, b := kVerb.FindAllString(ru[0], -1), kVerb.FindAllString(en[0], -1); !slices.Equal(a, b) {
			t.Errorf("key %q takes %v in ru and %v in en", key, a, b)
		}
	}
}

func TestPageN(t *testing.T) {
	for n, want := range map[int]string{1: "нашлось 1 товар", 3: "нашлось 3 товара",
		5: "нашлось 5 товаров", 11: "нашлось 11 товаров", 22: "нашлось 22 товара"} {
		if got := PageN(LangRU, "found", n); got != want {
			t.Errorf("ru %d: %q", n, got)
		}
	}
	if got := PageN(LangEN, "found", 1); got != "1 product found" {
		t.Errorf("en 1: %q", got)
	}
	if got := PageN(LangEN, "found", 11); got != "11 products found" {
		t.Errorf("en 11: %q", got)
	}
}

var (
	kTemplateKey = regexp.MustCompile(`\.(?:T|TH|N) "([a-z_]+)"`)
	kGoKey       = regexp.MustCompile(`(?:i18n\.PageN?\([^,]+, |\bt\()"([a-z_]+)"`)
)

// A template asking for a key we lack prints the key itself; a phrase nobody asks for rots.
func TestPageKeysMatchTheStorefront(t *testing.T) {
	var src strings.Builder
	used := map[string]bool{}
	files, _ := filepath.Glob("../storefront/templates/*.html")
	goFiles, _ := filepath.Glob("../storefront/*.go")
	for _, f := range append(files, goFiles...) {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
		re := kTemplateKey
		if strings.HasSuffix(f, ".go") {
			re = kGoKey
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
		}
	}
	if len(files) == 0 || len(used) == 0 {
		t.Fatal("storefront sources not found")
	}
	for key := range used {
		if _, ok := kPage[key]; !ok {
			t.Errorf("storefront uses %q, which has no translation", key)
		}
	}
	for key := range kPage {
		if !strings.Contains(src.String(), `"`+key+`"`) {
			t.Errorf("phrase %q is used nowhere", key)
		}
	}
}

// The order stores the plain consent, so it must be the very sentence the buyer ticked.
func TestStoredConsentIsTheOneOnScreen(t *testing.T) {
	tags := regexp.MustCompile(`<[^>]+>`)
	for _, lang := range []string{LangRU, LangEN} {
		shown := tags.ReplaceAllString(Page(lang, "consent_order_html"), "")
		if stored := Page(lang, "consent_order"); shown != stored {
			t.Errorf("%s: shown %q, stored %q", lang, shown, stored)
		}
	}
}
