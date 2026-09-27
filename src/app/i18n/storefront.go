package i18n

import (
	"fmt"
	"strings"
)

// kPage is what the buyer reads on the storefront. Kept apart from kMessages so
// TIfKey never takes a platform's stored text for one of these keys.
// Plural entries hold their forms split by "|": ru one|few|many, en one|other.
var kPage = map[string][2]string{
	// Layout
	"out_of_stock":       {"Нет в наличии", "Out of stock"},
	"page_prev":          {"← Назад", "← Back"},
	"page_next":          {"Дальше →", "Next →"},
	"page_of":            {"Страница %d из %d", "Page %d of %d"},
	"nothing_found":      {"По запросу «%s» ничего не нашлось.", "Nothing found for “%s”."},
	"whole_catalog":      {"Весь каталог", "Whole catalogue"},
	"whole_catalog_lc":   {"весь каталог", "whole catalogue"},
	"coming_soon":        {"Товары скоро появятся.", "Products are coming soon."},
	"aria_crumbs":        {"Хлебные крошки", "Breadcrumbs"},
	"aria_filters":       {"Сортировка и фильтры", "Sorting and filters"},
	"catalog":            {"Каталог", "Catalogue"},
	"categories":         {"Категории", "Categories"},
	"search_placeholder": {"Поиск по товарам", "Search products"},
	"search_button":      {"Найти", "Search"},
	"cart":               {"Корзина", "Cart"},
	"promise_ok":         {"Понятно", "Got it"},
	"sub_title":          {"Новинки и скидки на почту", "New arrivals and discounts by email"},
	"sub_text": {
		"Пишем редко - только когда есть что показать.",
		"We write rarely - only when there is something to show.",
	},
	"sub_done": {
		"Подписались. Письма будут приходить на %s.",
		"Subscribed. Emails will go to %s.",
	},
	"sub_bad_email":   {"Проверьте адрес почты.", "Check the email address."},
	"sub_placeholder": {"ваша почта", "your email"},
	"sub_button":      {"Подписаться", "Subscribe"},
	"sub_hint": {
		`Нажимая кнопку, вы соглашаетесь получать письма магазина и принимаете <a href="/privacy">политику обработки данных</a>. Отписаться можно одной ссылкой из любого письма.`,
		`By pressing the button you agree to receive the shop's emails and accept the <a href="/privacy">data processing policy</a>. You can unsubscribe with one link in any email.`,
	},
	"email":            {"Почта", "Email"},
	"phone":            {"Телефон", "Phone"},
	"delivery_payment": {"Доставка и оплата", "Delivery and payment"},
	"offer":            {"Оферта", "Terms of sale"},
	"privacy_short":    {"Обработка данных", "Privacy"},
	"contacts":         {"Контакты", "Contacts"},
	"to_catalog":       {"Перейти в каталог", "Go to the catalogue"},
	"default_shop":     {"Магазин", "Shop"},

	// Listing
	"title_search":  {"%s — поиск", "%s — search"},
	"title_catalog": {"%s — каталог товаров", "%s — product catalogue"},
	"title_buy":     {"%s — купить в %s", "%s — buy at %s"},
	"title_page":    {" - страница %d", " - page %d"},
	"meta_page":     {" Страница %d.", " Page %d."},
	"index_meta": {
		"%s: каталог товаров, цены, наличие.",
		"%s: product catalogue, prices, availability.",
	},
	"category_meta": {
		"%s — %s в магазине «%s»: цены, наличие, доставка.",
		"%s — %s at %s: prices, availability, delivery.",
	},
	"categories_meta": {
		"Все категории магазина «%s»: %d разделов каталога.",
		"All categories of %s: %d catalogue sections.",
	},
	"search_heading": {"Поиск: %s", "Search: %s"},
	"found": {
		"нашлось %d товар|нашлось %d товара|нашлось %d товаров",
		"%d product found|%d products found",
	},
	"sort_default":  {"по умолчанию", "default"},
	"sort_cheap":    {"сначала дешёвые", "cheapest first"},
	"sort_dear":     {"сначала дорогие", "most expensive first"},
	"sort_title":    {"по названию", "by name"},
	"in_stock_only": {"только в наличии", "in stock only"},

	// Product
	"photo_n":         {"Фото %d", "Photo %d"},
	"sku":             {"Артикул: %s", "SKU: %s"},
	"pcs_in_product":  {"Штук в товаре", "Pieces per item"},
	"per_pc":          {"%s/шт", "%s/pc"},
	"in_sets":         {"Также наборами:", "Also in sets:"},
	"set_contents":    {"Состав набора", "Set contents"},
	"qty_pcs":         {"%d шт", "%d pcs"},
	"specs":           {"Характеристики", "Specifications"},
	"weight":          {"Вес", "Weight"},
	"size":            {"Габариты", "Dimensions"},
	"size_cm":         {"%s × %s × %s см", "%s × %s × %s cm"},
	"weight_kg":       {"%s кг", "%s kg"},
	"weight_g":        {"%d г", "%d g"},
	"yes":             {"да", "yes"},
	"no":              {"нет", "no"},
	"in_stock":        {"В наличии: %d шт", "In stock: %d pcs"},
	"quantity":        {"Количество", "Quantity"},
	"add_to_cart":     {"В корзину", "Add to cart"},
	"write_us":        {"Напишите нам — сообщим, когда товар вернётся.", "Write to us — we will let you know when it is back."},
	"pay_on_receipt":  {"Оплата при получении — без предоплаты", "Pay on receipt — no prepayment"},
	"confirm_by_call": {"Подтверждаем заказ звонком", "We confirm the order by phone"},
	"questions":       {"Вопросы:", "Questions:"},
	"invoice_hint": {
		"Нужен счёт для организации? Оформите заказ — в форме укажете название, %s и приложите реквизиты.",
		"Need an invoice for an organisation? Place an order — the form asks for the name, %s and the company details.",
	},
	"description":    {"Описание", "Description"},
	"order_telegram": {"Заказать в Telegram", "Order via Telegram"},
	"order_whatsapp": {"Заказать в WhatsApp", "Order via WhatsApp"},
	"buy_wb":         {"Купить на Wildberries", "Buy on Wildberries"},
	"buy_ozon":       {"Купить на Ozon", "Buy on Ozon"},
	"msg_order":      {"Здравствуйте! Хочу заказать: %s", "Hello! I would like to order: %s"},
	"msg_sku":        {", артикул %s", ", SKU %s"},
	"msg_price":      {", цена %s", ", price %s"},

	// Promise line and footer terms
	"promise_free_from": {"Доставка бесплатно от %s", "Free delivery from %s"},
	"promise_days": {
		"доставим за %d день|доставим за %d дня|доставим за %d дней",
		"delivered in %d day|delivered in %d days",
	},
	"promise_return": {
		"возврат в течение %d дня|возврат в течение %d дней|возврат в течение %d дней",
		"returns within %d day|returns within %d days",
	},
	"terms_free_from": {"бесплатная доставка от %s", "free delivery from %s"},
	"terms_days": {
		"доставка за %d день|доставка за %d дня|доставка за %d дней",
		"delivery in %d day|delivery in %d days",
	},
	"terms_return": {
		"возврат %d день|возврат %d дня|возврат %d дней",
		"returns: %d day|returns: %d days",
	},

	// FAQ on the delivery page: answers assembled from the stated numbers
	"faq_title":      {"Частые вопросы", "Frequently asked questions"},
	"faq_delivery_q": {"Сколько стоит доставка?", "How much is delivery?"},
	"faq_delivery_a": {"Доставка стоит %s.", "Delivery costs %s."},
	"faq_delivery_free_a": {
		"От %s доставим бесплатно.",
		"From %s we deliver free of charge.",
	},
	"faq_delivery_free_only_a": {
		"От %s доставка бесплатная, при меньшей сумме стоимость назовём при подтверждении заказа.",
		"From %s delivery is free; below that we name the cost when we confirm the order.",
	},
	"faq_days_q": {"Как быстро вы доставляете?", "How fast do you deliver?"},
	"faq_days_a": {
		"Обычно за %d день после подтверждения заказа.|Обычно за %d дня после подтверждения заказа.|Обычно за %d дней после подтверждения заказа.",
		"Usually in %d day after the order is confirmed.|Usually in %d days after the order is confirmed.",
	},
	"faq_return_q": {"Можно ли вернуть товар?", "Can I return an item?"},
	"faq_return_a": {
		"Да, в течение %d дня после получения.|Да, в течение %d дней после получения.|Да, в течение %d дней после получения.",
		"Yes, within %d day of receiving it.|Yes, within %d days of receiving it.",
	},
	"faq_order_q": {"Как оформить заказ?", "How do I place an order?"},
	"faq_order_a": {
		"Положите товар в корзину и оставьте имя и телефон или почту - регистрация не нужна. Мы перезвоним и подтвердим заказ.",
		"Put the item in the cart and leave a name and a phone or an email - no registration needed. We will call back and confirm the order.",
	},
	"faq_pay_q": {"Как оплатить?", "How do I pay?"},
	"faq_pay_a": {
		"Оплата при получении, предоплата не нужна.",
		"Payment on receipt, no prepayment needed.",
	},
	"faq_org_q": {"Работаете ли вы с организациями?", "Do you work with companies?"},
	"faq_org_a": {
		"Да. В форме заказа выберите «организация», укажите название и УНП или ИНН и приложите реквизиты - выставим счёт.",
		"Yes. Choose “company” in the order form, give the name and the tax id and attach the details - we will issue an invoice.",
	},
	"faq_contact_q": {"Как с вами связаться?", "How do I contact you?"},
	"faq_contact_a": {"Позвоните по телефону %s.", "Call us at %s."},

	// Cart
	"cart_ordered": {
		"Спасибо! Заказ принят, мы свяжемся с вами по телефону.",
		"Thank you! Your order has been received, we will call you.",
	},
	"cart_sold_out": {
		"Пока вы оформляли заказ, «%s» закончился — мы убрали его из корзины. Заказ не оформлен, проверьте состав и подтвердите ещё раз.",
		"While you were placing the order, “%s” sold out — we removed it from the cart. The order was not placed: check the cart and confirm again.",
	},
	"cart_dropped": {
		"Часть товаров закончилась и была убрана из корзины.",
		"Some items sold out and were removed from the cart.",
	},
	"qty_less":         {"Меньше", "Less"},
	"qty_more":         {"Больше", "More"},
	"remove":           {"Удалить", "Remove"},
	"cart_items":       {"Товары, %d шт.:", "Items, %d pcs:"},
	"delivery":         {"Доставка:", "Delivery:"},
	"delivery_free":    {"бесплатно", "free"},
	"delivery_unknown": {"рассчитаем при подтверждении заказа", "calculated when we confirm the order"},
	"free_from":        {"Бесплатно от %s", "Free from %s"},
	"payable":          {"К оплате:", "To pay:"},
	"checkout":         {"Оформление заказа", "Checkout"},
	"no_contact": {
		"Заполните телефон или почту — иначе мы не сможем подтвердить заказ.",
		"Enter a phone or an email — otherwise we cannot confirm the order.",
	},
	"pay_and_call": {
		"Оплата при получении. Мы перезвоним и подтвердим.",
		"Pay on receipt. We will call you back to confirm.",
	},
	"bad_org": {
		"Укажите название организации и %s — по ним продавец выставит счёт.",
		"Enter the organisation's name and %s — the seller needs them to issue an invoice.",
	},
	"private_person":      {"Частное лицо", "Private person"},
	"organisation":        {"Организация", "Organisation"},
	"your_name":           {"Ваше имя", "Your name"},
	"name_placeholder":    {"Иван", "John"},
	"contact_hint":        {"Оставьте телефон или почту — можно и то, и другое. На почту пришлём подтверждение заказа.", "Leave a phone or an email — or both. We will send the order confirmation by email."},
	"comment":             {"Комментарий", "Comment"},
	"comment_placeholder": {"Удобное время для звонка, пожелания к доставке", "A convenient time to call, delivery wishes"},
	"no_consent": {
		"Чтобы оформить заказ, примите оферту и согласие на обработку данных.",
		"To place the order, accept the terms of sale and the consent to data processing.",
	},
	"consent_order_html": {
		`Принимаю условия <a href="/offer">публичной оферты</a> и даю согласие на <a href="/privacy">обработку персональных данных</a>.`,
		`I accept the <a href="/offer">terms of sale</a> and consent to the <a href="/privacy">processing of my personal data</a>.`,
	},
	"subscribe_tick": {
		"Хочу получать письма магазина: новинки, скидки и полезные материалы.",
		"I want to receive the shop's emails: new arrivals, discounts and useful materials.",
	},
	"place_order":      {"Оформить заказ", "Place order"},
	"cart_empty":       {"Корзина пуста.", "The cart is empty."},
	"org_name":         {"Название организации", "Organisation name"},
	"org_placeholder":  {"ООО «Ромашка»", "Acme Ltd"},
	"tax_id_rub":       {"ИНН", "INN (taxpayer number)"},
	"tax_id_byn":       {"УНП", "UNP (taxpayer number)"},
	"requisites_file":  {"Реквизиты файлом", "Company details as a file"},
	"requisites_hint":  {"Необязательно. Можно приложить карточку предприятия — или просто написать реквизиты в комментарии.", "Optional. Attach the company details card — or just write the details in the comment."},
	"feed_catch_all":   {"Товары", "Products"},
	"offer_title":      {"Публичная оферта", "Terms of sale (public offer)"},
	"privacy_title":    {"Политика обработки персональных данных", "Personal data processing policy"},
	"notfound_title":   {"Страница не найдена", "Page not found"},
	"notfound_heading": {"Такой страницы нет", "There is no such page"},
	"notfound_text": {
		"Возможно, товар закончился или адрес изменился. Поищите его по названию или загляните в каталог.",
		"Perhaps the product sold out or the address changed. Search for it by name or browse the catalogue.",
	},
	"unsub_title":   {"Отписка", "Unsubscribe"},
	"unsub_heading": {"Вы отписаны", "You are unsubscribed"},
	"unsub_text": {
		"Писем от магазина «%s» больше не будет. Если это вышло случайно, подписаться снова можно внизу любой страницы.",
		"There will be no more emails from %s. If this was a mistake, you can subscribe again at the bottom of any page.",
	},

	// Stored word for word with the order or the address: what the buyer agreed to.
	"consent_order": {
		"Принимаю условия публичной оферты и даю согласие на обработку персональных данных.",
		"I accept the terms of sale and consent to the processing of my personal data.",
	},
	"consent_order_subscribe": {
		"Хочу получать письма магазина: новинки, скидки и полезные материалы. Отписаться можно ссылкой из любого письма.",
		"I want to receive the shop's emails: new arrivals, discounts and useful materials. I can unsubscribe with a link in any email.",
	},
	"consent_subscribe": {
		"Согласен получать письма магазина: новинки, скидки и полезные материалы. Принимаю политику обработки персональных данных. Отписаться можно ссылкой из любого письма.",
		"I agree to receive the shop's emails: new arrivals, discounts and useful materials. I accept the personal data processing policy. I can unsubscribe with a link in any email.",
	},

	// llms.txt
	"llms_intro": {
		"Интернет-магазин: %d товаров в наличии. Оплата при получении, без предоплаты; заказ подтверждается звонком. Цены в %s.",
		"Online shop: %d products in stock. Pay on receipt, no prepayment; orders are confirmed by phone. Prices in %s.",
	},
	"llms_order": {
		"Заказ оформляется на сайте без регистрации: корзина → имя и телефон или почта.",
		"Orders are placed on the site without registration: cart → name and a phone or an email.",
	},
	"llms_phone":          {"Телефон магазина: %s", "Shop phone: %s"},
	"llms_count":          {"%d товаров", "%d products"},
	"llms_pages":          {"Страницы", "Pages"},
	"llms_all_categories": {"Все категории", "All categories"},
	"llms_sitemap":        {"Карта сайта", "Sitemap"},
	"llms_sitemap_note":   {"каждый товар с датой обновления", "every product with its update date"},
	"llms_product": {
		"Страница товара (%s/p/<slug>) отдаётся сервером без скриптов и несёт разметку schema.org/Product с ценой и наличием.",
		"A product page (%s/p/<slug>) is rendered by the server without scripts and carries schema.org/Product markup with price and availability.",
	},
}

func pagePhrase(lang, key string) string {
	m, ok := kPage[key]
	if !ok {
		return key
	}
	if lang == LangEN {
		return m[1]
	}
	return m[0]
}

// Page falls back to Russian like T; without args the phrase is not a format string.
func Page(lang, key string, args ...any) string {
	s := pagePhrase(lang, key)
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// PageN picks the plural form for n: ru 1 день, 2 дня, 5 дней; en 1 day, 2 days.
func PageN(lang, key string, n int) string {
	forms := strings.Split(pagePhrase(lang, key), "|")
	i := 0
	if lang == LangEN {
		if n != 1 {
			i = 1
		}
	} else {
		switch {
		case n%10 == 1 && n%100 != 11:
			i = 0
		case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
			i = 1
		default:
			i = 2
		}
	}
	return fmt.Sprintf(forms[min(i, len(forms)-1)], n)
}
