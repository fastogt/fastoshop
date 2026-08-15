package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// categoryRow is a node of the tree as the admin list shows it: the path, how
// many goods hang below it and the owner's own text for the page.
type categoryRow struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
	Body  string `json:"body"`
}

type categoryListResponse struct {
	Categories []categoryRow `json:"categories"`
	Total      int           `json:"total"`
}

// CategoryList is the whole tree in one response, unpaged on purpose: a
// catalogue of 24 000 products has hundreds of categories, not thousands, and
// the screen is a place to look for the ones still without a text.
func (h *Handler) CategoryList(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.db.VisibleCategories()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	texts, err := h.db.CategoryTexts()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	rows := make([]categoryRow, 0, len(nodes))
	for _, n := range nodes {
		if q != "" && !strings.Contains(strings.ToLower(n.Path), q) {
			continue
		}
		rows = append(rows, categoryRow{Path: n.Path, Count: n.Count, Body: texts[n.Path]})
	}
	writeOK(w, categoryListResponse{Categories: rows, Total: len(rows)})
}

type categoryTextRequest struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

func (h *Handler) SetCategoryText(w http.ResponseWriter, r *http.Request) {
	var req categoryTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "bad json")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeBadRequest(w, "empty path")
		return
	}
	if err := h.db.SetCategoryText(req.Path, req.Body); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "saved"})
}

// DeleteCategoryText drops the owner's text. Saving an empty field does the
// same, but a delete the caller has to name is what a REST client and the next
// developer expect to find.
func (h *Handler) DeleteCategoryText(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeBadRequest(w, "empty path")
		return
	}
	if err := h.db.SetCategoryText(path, ""); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "deleted"})
}

type categoryDraftResponse struct {
	Body string `json:"body"`
}

// CategoryDraft assembles a starting text out of what the shop already knows:
// the name of the category, what is in it, what it costs and the owner's own
// delivery terms. Nothing is invented and nothing is published — the draft
// lands in the text box for the owner to rewrite. An empty box is why category
// texts never get written; a paragraph with real goods and real prices in it is
// something a person can edit in a minute.
func (h *Handler) CategoryDraft(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeBadRequest(w, "empty path")
		return
	}
	sample, err := h.db.SampleCategory(path)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sample.Count == 0 {
		writeBadRequest(w, "empty category")
		return
	}
	nodes, err := h.db.VisibleCategories()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	settings, err := h.db.GetSettings()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, categoryDraftResponse{
		Body: draftText(h.lang(), path, sample, childNames(nodes, path), settings)})
}

func childNames(nodes []database.CategoryNode, path string) []string {
	prefix := path + database.CategorySep
	depth := len(strings.Split(path, database.CategorySep)) + 1
	var out []string
	for _, n := range nodes {
		if !strings.HasPrefix(n.Path, prefix) {
			continue
		}
		segments := strings.Split(n.Path, database.CategorySep)
		if len(segments) == depth {
			out = append(out, segments[len(segments)-1])
		}
	}
	return out
}

// kDraftAssortment caps the assortment sentence by length, not by count: a
// feed's category names carry commas of their own ("Комоды, тумбы, консоли"),
// so five of them can read like a dump of the catalogue.
const kDraftAssortment = 90

func draftText(lang, path string, s *database.CategorySample,
	children []string, settings *database.Settings) string {
	segments := strings.Split(path, database.CategorySep)
	name := segments[len(segments)-1]
	var b strings.Builder
	fmt.Fprintf(&b, i18n.T(lang, i18n.KeyDraftIntro), name, settings.ShopName, s.Count)

	// Subcategories name the assortment better than product titles ever will;
	// a leaf has none, so its goods are read for their nouns instead.
	items := children
	if len(items) == 0 {
		items = titleNouns(s.Titles)
	}
	items = fitItems(items, kDraftAssortment)
	if len(items) > 0 {
		fmt.Fprintf(&b, " "+i18n.T(lang, i18n.KeyDraftAssortment),
			strings.ToLower(strings.Join(items, ", ")))
	}
	if s.MaxPrice > s.MinPrice {
		fmt.Fprintf(&b, " "+i18n.T(lang, i18n.KeyDraftPriceRange),
			money(s.MinPrice), money(s.MaxPrice), settings.Sign())
	} else if s.MaxPrice > 0 {
		fmt.Fprintf(&b, " "+i18n.T(lang, i18n.KeyDraftPriceOne),
			money(s.MaxPrice), settings.Sign())
	}
	if terms := firstSentences(settings.Terms, 2); terms != "" {
		b.WriteString("\n\n" + terms)
	}
	return b.String()
}

// fitItems keeps whole names while the sentence stays readable.
func fitItems(items []string, limit int) []string {
	n := 0
	for i, item := range items {
		n += len([]rune(item)) + 2
		if n > limit {
			return items[:max(i, 1)]
		}
	}
	return items
}

func money(minor int64) string {
	if minor%100 == 0 {
		return strconv.FormatInt(minor/100, 10)
	}
	return fmt.Sprintf("%.2f", float64(minor)/100)
}

// titleNouns pulls the thing being sold out of a supplier's title. Feeds write
// "НЕСОРТ 96366 Тёрка пластмассовая 5 насадок 12,5х30см, ..." — the goods are
// the words before the first number and the first comma, and everything else is
// packaging, size and the supplier's own bookkeeping.
func titleNouns(titles []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, title := range titles {
		head, _, _ := strings.Cut(title, ",")
		var words []string
		for _, word := range strings.Fields(head) {
			word = strings.Trim(word, "\"«»'")
			// A brand in quotes, a number, an article code, the supplier's own
			// shouting and a latin brand name are not the thing being sold: the
			// noun comes after them ("ZEIDAN набор кастрюль" → "набор кастрюль").
			if word == "" || strings.ContainsAny(word, "0123456789") ||
				isShout(word) || isLatin(word) {
				continue
			}
			// A title cut mid-phrase reads worse than a single noun: "кастрюля из"
			// is not a category, "кастрюля" is.
			if kStopWords[strings.ToLower(word)] {
				break
			}
			words = append(words, word)
			if len(words) == 2 {
				break
			}
		}
		if len(words) == 0 {
			continue
		}
		key := strings.ToLower(words[0])
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, strings.Join(words, " "))
	}
	return out
}

// kStopWords end a phrase: everything after them belongs to the description,
// not to the name.
var kStopWords = map[string]bool{
	"из": true, "для": true, "с": true, "со": true, "на": true, "в": true,
	"и": true, "по": true, "под": true, "от": true, "без": true,
	"of": true, "for": true, "with": true, "in": true, "and": true,
}

// isLatin — a word written in the other alphabet than the goods around it. In a
// Russian catalogue that is the brand, and a brand is not a category.
func isLatin(word string) bool {
	for _, r := range word {
		if r > 127 {
			return false
		}
	}
	return true
}

// isShout — a word in capitals is the supplier's marker ("НЕСОРТ", "АКЦИЯ"),
// not the name of the thing.
func isShout(word string) bool {
	runes := []rune(word)
	if len(runes) < 3 {
		return false
	}
	return strings.ToUpper(word) == word
}

// firstSentences takes the beginning of the owner's terms. Lines that are a
// single word are headings ("Доставка") and read as noise inside a paragraph.
func firstSentences(text string, n int) string {
	var body []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || len(strings.Fields(line)) == 1 {
			continue
		}
		body = append(body, line)
	}
	joined := strings.Join(body, " ")
	for i, r := range joined {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		n--
		if n == 0 {
			return joined[:i+1]
		}
	}
	return joined
}
