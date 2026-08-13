// Package i18n renders the text the shop owner reads: API error messages and
// the emails we send them. A shop has exactly one owner, so the shop's language
// setting is the owner's language — there is no per-request negotiation and no
// need to hand error codes to the client.
//
// The storefront is deliberately absent from here: it speaks the language of the
// products the seller sells.
package i18n

import (
	"errors"
	"fmt"
)

const (
	LangRU = "ru"
	LangEN = "en"
)

func IsValidLang(l string) bool { return l == LangRU || l == LangEN }

// Message keys. Values carry %-verbs where the caller substitutes; both
// translations of one key must take the same arguments in the same order.
const (
	KeyOrderStockGone      = "order_stock_gone"
	KeyCSVParseFailed      = "csv_parse_failed"
	KeyTestMailSubject     = "test_mail_subject"
	KeyTestMailBody        = "test_mail_body"
	KeyNewOrderSubject     = "new_order_subject"
	KeyOrderTotal          = "order_total"
	KeyOrderName           = "order_name"
	KeyOrderPhone          = "order_phone"
	KeyOrderComment        = "order_comment"
	KeyOzonBadCurrency     = "ozon_bad_currency"
	KeyOzonNoKeys          = "ozon_no_keys"
	KeyOzonNegativePrice   = "ozon_negative_price"
	KeyOzonNegativeMarkup  = "ozon_negative_markup"
	KeyOzonNotLinked       = "ozon_not_linked"
	KeyOzonBadWarehouse    = "ozon_bad_warehouse"
	KeyOzonPushBusy        = "ozon_push_busy"
	KeyOzonNoAnswer        = "ozon_no_answer"
	KeyOzonUnknownReply    = "ozon_unknown_reply"
	KeyOzonNothingSelected = "ozon_nothing_selected"
	KeyBadCoefficient      = "bad_coefficient"
	KeyOzonBadRules        = "ozon_bad_rules"
	KeySupplierRequired    = "supplier_required"
	KeyBadStock            = "bad_stock"
	KeyNothingSelected     = "nothing_selected"
	KeyJobBusy             = "job_busy"
	KeyBadCurrency         = "bad_currency"
	KeyYMLBadURL           = "yml_bad_url"
	KeyYMLBadStatus        = "yml_bad_status"
	KeyYMLTooBig           = "yml_too_big"
	KeyYMLBadXML           = "yml_bad_xml"
)

var kMessages = map[string][2]string{
	// [0] = ru, [1] = en
	KeyOrderStockGone: {
		"Нельзя вернуть заказ в работу: «%s» закончился",
		"Cannot reopen the order: %q is out of stock",
	},
	KeyCSVParseFailed: {"ПАРСИНГ НЕ УДАЛСЯ", "PARSING FAILED"},
	KeyTestMailSubject: {
		"fastoshop: проверка почты", "fastoshop: mail check",
	},
	KeyTestMailBody: {
		"Письма о заказах будут приходить сюда.",
		"Order notifications will arrive here.",
	},
	KeyNewOrderSubject: {"Новый заказ #%d", "New order #%d"},
	KeyOrderTotal:      {"Итого", "Total"},
	KeyOrderName:       {"Имя", "Name"},
	KeyOrderPhone:      {"Телефон", "Phone"},
	KeyOrderComment:    {"Комментарий", "Comment"},
	KeyOzonBadCurrency: {
		"валюта должна быть RUB или BYN", "currency must be RUB or BYN",
	},
	KeyOzonNoKeys: {
		"сначала сохраните Client-Id и Api-Key",
		"save the Client-Id and Api-Key first",
	},
	KeyOzonNegativePrice: {
		"цена не может быть отрицательной", "price cannot be negative",
	},
	KeyOzonNegativeMarkup: {
		"наценка не может быть отрицательной", "markup cannot be negative",
	},
	KeyOzonNotLinked: {
		"товар не связан с карточкой Ozon",
		"the product is not linked to an Ozon listing",
	},
	KeyOzonBadWarehouse: {
		"warehouse_id должен быть числом: %q", "warehouse_id must be a number: %q",
	},
	KeyOzonPushBusy: {
		"отправка на Ozon уже идёт", "a push to Ozon is already running",
	},
	KeyOzonNoAnswer: {
		"Ozon не ответил по этому артикулу", "Ozon did not answer for this article",
	},
	KeyOzonUnknownReply: {"неизвестный ответ Ozon", "unrecognised Ozon reply"},
	KeyOzonNothingSelected: {
		"не выбрано ни одного товара", "no products selected",
	},
	KeyOzonBadRules: {
		"проверьте лестницу наценки: множители больше нуля и ровно одна строка «и выше»",
		"check the markup ladder: multipliers above zero and exactly one \"and above\" row",
	},
	KeyBadStock: {
		"остаток не может быть отрицательным", "stock cannot be negative",
	},
	KeyNothingSelected: {
		"не выбрано ни одной строки", "no rows selected",
	},
	KeyJobBusy: {
		"дождитесь окончания текущей задачи",
		"wait for the running task to finish",
	},
	KeyBadCurrency: {
		"этой валютой магазин торговать не умеет",
		"the shop cannot trade in this currency",
	},
	KeySupplierRequired: {
		"укажите поставщика: импорт обновляет только свою группу товаров",
		"name the supplier: an import only updates its own group of products",
	},
	KeyBadCoefficient: {
		"коэффициент должен быть больше нуля и не больше 1000",
		"the coefficient must be greater than zero and at most 1000",
	},
	KeyYMLBadURL: {
		"ссылка должна начинаться с http:// или https://",
		"the link must start with http:// or https://",
	},
	KeyYMLBadStatus: {"сервер выгрузки ответил %d", "the feed server answered %d"},
	KeyYMLTooBig: {
		"выгрузка больше %d МБ — такой каталог мы не тянем",
		"the feed is over %d MB — we do not pull a catalogue that size",
	},
	KeyYMLBadXML: {"не удалось разобрать XML выгрузки", "could not parse the feed XML"},
}

// T returns the message for lang, falling back to Russian: an unknown language
// must not blank out an error the owner needs to read.
func T(lang, key string) string {
	m, ok := kMessages[key]
	if !ok {
		return key
	}
	if lang == LangEN {
		return m[1]
	}
	return m[0]
}

// KeyError is an error the owner will read: a message key plus its arguments.
// Error() renders English for logs and wrapping; the handler localizes it on
// the way out with Localize.
type KeyError struct {
	Key  string
	Args []any
}

func (e *KeyError) Error() string { return fmt.Sprintf(T(LangEN, e.Key), e.Args...) }

// Localize renders err for the owner: KeyErrors in their language, anything
// else as is — someone else's error text is not ours to rewrite.
func Localize(lang string, err error) string {
	var ke *KeyError
	if errors.As(err, &ke) {
		return fmt.Sprintf(T(lang, ke.Key), ke.Args...)
	}
	return err.Error()
}

// TIfKey translates a string only if it is one of our keys. Errors stored in the
// database mix two origins: our own sentinels, which must follow the owner's
// language, and text the marketplace produced, which we pass through untouched —
// translating someone else's error would be inventing it.
func TIfKey(lang, s string) string {
	if _, ok := kMessages[s]; ok {
		return T(lang, s)
	}
	return s
}
