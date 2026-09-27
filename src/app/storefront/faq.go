package storefront

import (
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// A buyer about to pay a stranger asks the same five things, and an answer
// engine quotes whatever answers them on the page. Both are served from the
// numbers the owner already stated - an invented delivery term here would be
// the shop's obligation, not our guess, so a fact left unstated asks nothing.
type faqVM struct{ Q, A string }

func faq(shop *database.Settings) []faqVM {
	t := func(key string, args ...any) string { return i18n.Page(shop.Lang, key, args...) }
	sum := func(v int64) string { return priceStr(v) + " " + shop.Sign() }
	out := make([]faqVM, 0, 6)
	switch {
	case shop.DeliveryCost > 0:
		a := t("faq_delivery_a", sum(shop.DeliveryCost))
		if shop.DeliveryFreeFrom > 0 {
			a += " " + t("faq_delivery_free_a", sum(shop.DeliveryFreeFrom))
		}
		out = append(out, faqVM{t("faq_delivery_q"), a})
	case shop.DeliveryFreeFrom > 0:
		// Free above a sum and nothing stated below it: the cart says the same.
		out = append(out, faqVM{t("faq_delivery_q"), t("faq_delivery_free_only_a", sum(shop.DeliveryFreeFrom))})
	}
	if shop.DeliveryDays > 0 {
		out = append(out, faqVM{t("faq_days_q"), i18n.PageN(shop.Lang, "faq_days_a", shop.DeliveryDays)})
	}
	if shop.ReturnDays > 0 {
		out = append(out, faqVM{t("faq_return_q"), i18n.PageN(shop.Lang, "faq_return_a", shop.ReturnDays)})
	}
	// Only a shop that takes the order itself can say how it is placed and paid.
	if shop.CartEnabled {
		out = append(out, faqVM{t("faq_order_q"), t("faq_order_a")},
			faqVM{t("faq_pay_q"), t("faq_pay_a")})
		if shop.OrgOnly() || shop.OrgChoice() {
			out = append(out, faqVM{t("faq_org_q"), t("faq_org_a")})
		}
	}
	if shop.ShopPhone != "" {
		out = append(out, faqVM{t("faq_contact_q"), t("faq_contact_a", shop.ShopPhone)})
	}
	return out
}
