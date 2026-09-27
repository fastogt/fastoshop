package storefront

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// The delivery page answers what a buyer asks before paying a stranger, and an
// assistant reads the same pairs as data. Every answer comes from a number the
// owner stated: what they left at zero is not asked about and not promised.
func TestFAQAnswersOnlyStatedFacts(t *testing.T) {
	d, h := setup(t)
	s, _ := d.GetSettings()
	s.Terms = "Возим по стране."
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	// The cart is on by default, so how to order and how to pay are answerable;
	// nothing about delivery is, because no number for it was stated.
	bare := get(t, h, "/info")
	for _, gone := range []string{"Сколько стоит доставка", "Как быстро", "вернуть товар"} {
		if strings.Contains(bare, gone) {
			t.Errorf("a shop that stated no numbers still asked %q", gone)
		}
	}
	if !strings.Contains(bare, "Оплата при получении, предоплата не нужна.") {
		t.Error("a shop with its own checkout does not say how it is paid")
	}

	s.Currency = database.ShopCurrencyBYN
	s.DeliveryCost, s.DeliveryFreeFrom, s.DeliveryDays, s.ReturnDays = 590, 5000, 3, 14
	s.ShopPhone = "+375 29 000-00-00"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/info")
	for _, want := range []string{
		"Доставка стоит 5.90 Br.", "От 50.00 Br доставим бесплатно.",
		"Обычно за 3 дня после подтверждения заказа.",
		"Да, в течение 14 дней после получения.",
		"Позвоните по телефону +375 29 000-00-00.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not answer %q", want)
		}
	}
	s.CartEnabled = false
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	// Without a checkout of its own the shop may not promise how an order is paid.
	if noCart := get(t, h, "/info"); strings.Contains(noCart, "Оплата при получении") {
		t.Error("a shop without its own checkout promised payment on receipt")
	}

	var page struct {
		Type       string `json:"@type"`
		MainEntity []struct {
			Name   string `json:"name"`
			Answer struct {
				Text string `json:"text"`
			} `json:"acceptedAnswer"`
		} `json:"mainEntity"`
	}
	start := strings.Index(body, `{
  "@context": "https://schema.org",
  "@type": "FAQPage"`)
	if start < 0 {
		t.Fatal("the page carries no FAQ markup")
	}
	raw := body[start:]
	end := strings.Index(raw, "</script>")
	if end < 0 {
		t.Fatal("the FAQ block is not closed")
	}
	raw = raw[:end]
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("the markup is not valid JSON: %v", err)
	}
	if page.Type != "FAQPage" || len(page.MainEntity) != 6 {
		t.Fatalf("got %s with %d questions", page.Type, len(page.MainEntity))
	}
	for _, q := range page.MainEntity {
		if q.Name == "" || q.Answer.Text == "" {
			t.Errorf("an empty pair in the markup: %q -> %q", q.Name, q.Answer.Text)
		}
	}
}
