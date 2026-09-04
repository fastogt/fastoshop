package i18n

import "testing"

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

// Errors stored in the database mix our sentinels with text from the platform:
// ours get translated, the platform's is passed through untouched.
func TestTIfKeyLeavesForeignTextAlone(t *testing.T) {
	if got := TIfKey(LangEN, KeyOzonNoAnswer); got != "Ozon did not answer for this article" {
		t.Errorf("own key not translated: %q", got)
	}
	const platform = "Товар не найден в системе"
	if got := TIfKey(LangEN, platform); got != platform {
		t.Errorf("platform text must survive: %q", got)
	}
}
