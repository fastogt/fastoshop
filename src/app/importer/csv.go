package importer

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"
)

// CSV is the shop's own template: a fixed set of columns the owner fills in a
// spreadsheet. No guessing at someone else's layout — the file's author is our
// user, so we hand them the shape instead of trying to infer it.
type CSV struct {
	Data []byte

	rows   []Item
	errs   int
	parsed bool
}

func (c *CSV) Name() string { return "csv" }

// Template is what the "Download template" button returns. Semicolon-separated
// and BOM-prefixed on purpose: that is what Russian Excel opens without turning
// the file into one column of mojibake.
func Template() []byte {
	return []byte("\xef\xbb\xbf" +
		"sku;title;description;price;stock;category;images\n" +
		"CH-201;Чайник эмалированный 2 л;Объём 2 л, индукция;2500.00;7;kuhnya;https://example.com/1.jpg|https://example.com/2.jpg\n")
}

// kCP1251 maps the upper half of windows-1251 to runes. A table beats pulling in
// x/text for one legacy charset — and this one is not optional: Russian Excel
// writes CSV in cp1251, and without it the whole catalogue arrives as mojibake.
var kCP1251 = []rune(
	"ЂЃ‚ѓ„…†‡€‰Љ‹ЊЌЋЏђ‘’“”•–—�™љ›њќћџ" +
		" ЎўЈ¤Ґ¦§Ё©Є«¬\u00ad®Ї°±Ііґµ¶·ё№є»јЅѕї" +
		"АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ" +
		"абвгдежзийклмнопрстуфхцчшщъыьэюя")

// decode returns the file as UTF-8. Detection is by validity rather than by a
// declared charset: spreadsheets do not declare one, and invalid UTF-8 in this
// part of the world means cp1251 in practice.
func decode(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if utf8.Valid(raw) {
		return string(raw)
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, ch := range raw {
		if ch < 0x80 {
			b.WriteByte(ch)
			continue
		}
		b.WriteRune(kCP1251[ch-0x80])
	}
	return b.String()
}

// delimiter guesses from the header line: Excel in a Russian locale writes
// semicolons, everyone else writes commas.
func delimiter(text string) rune {
	head, _, _ := strings.Cut(text, "\n")
	if strings.Count(head, ";") > strings.Count(head, ",") {
		return ';'
	}
	return ','
}

func (c *CSV) parse() {
	if c.parsed {
		return
	}
	c.parsed = true
	text := decode(c.Data)
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delimiter(text)
	// Rows with a stray extra separator must not abort the whole file.
	r.FieldsPerRecord = -1

	records, err := r.ReadAll()
	if err != nil {
		log.Warnf("csv: %v", err)
		c.errs++
		return
	}
	if len(records) == 0 {
		return
	}
	// Read by header name, not by position: a column moved in Excel must not
	// silently shift the price into the stock.
	col := map[string]int{}
	for i, name := range records[0] {
		col[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, want := range []string{"sku", "title", "price"} {
		if _, ok := col[want]; !ok {
			log.Warnf("csv: column %q is missing", want)
			c.errs++
			return
		}
	}

	get := func(row []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	for n, row := range records[1:] {
		if len(row) == 0 || strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		price, err := parseMoney(get(row, "price"))
		if err != nil {
			// The line number is what makes the log actionable: "row 418" sends
			// whoever reads it straight to the cell.
			log.Warnf("csv: row %d: price %q: %v", n+2, get(row, "price"), err)
			c.errs++
			continue
		}
		stock, _ := strconv.Atoi(get(row, "stock"))
		item := Item{
			SKU: get(row, "sku"), Title: get(row, "title"),
			Description: get(row, "description"), Price: price,
			Stock: max(stock, 0),
		}
		if imgs := get(row, "images"); imgs != "" {
			for _, u := range strings.Split(imgs, "|") {
				if u = strings.TrimSpace(u); u != "" {
					item.ImageURLs = append(item.ImageURLs, u)
				}
			}
		}
		c.rows = append(c.rows, item)
	}
}

// parseMoney accepts both decimal separators and ignores spaces used as
// thousands grouping: "1 234,50" is what a spreadsheet produces.
func parseMoney(s string) (int64, error) {
	s = strings.NewReplacer(" ", "", " ", "", ",", ".").Replace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("not a number")
	}
	if v < 0 {
		return 0, fmt.Errorf("negative")
	}
	return int64(v*100 + 0.5), nil
}

func (c *CSV) Count() (int, error) {
	c.parse()
	return len(c.rows), nil
}

func (c *CSV) Fetch() ([]Item, error) {
	c.parse()
	return c.rows, nil
}

// FetchErrors reports rows the file itself made unusable, so a broken cell shows
// up in the result instead of quietly shrinking the catalogue.
func (c *CSV) FetchErrors() int { return c.errs }
