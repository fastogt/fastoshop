package importer

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// CSV is the shop's own template: a fixed set of columns, no layout guessing.
type CSV struct {
	Data []byte

	rows   []Item
	errs   int
	parsed bool
}

func (c *CSV) Name() string { return "csv" }

// Semicolons and a BOM on purpose: Russian Excel opens anything else as mojibake.
func Template() []byte {
	return []byte("\xef\xbb\xbf" +
		"sku;title;description;price;stock;category;images;Цвет;Материал\n" +
		"CH-201;Чайник эмалированный 2 л;Объём 2 л, индукция;2500.00;7;kuhnya;" +
		"https://example.com/1.jpg|https://example.com/2.jpg;белый;эмаль\n")
}

// Upper half of windows-1251: Russian Excel writes CSV in that charset.
var kCP1251 = []rune(
	"ЂЃ‚ѓ„…†‡€‰Љ‹ЊЌЋЏђ‘’“”•–-�™љ›њќћџ" +
		" ЎўЈ¤Ґ¦§Ё©Є«¬\u00ad®Ї°±Ііґµ¶·ё№є»јЅѕї" +
		"АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ" +
		"абвгдежзийклмнопрстуфхцчшщъыьэюя")

// Spreadsheets declare no charset, so detection is by UTF-8 validity.
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

// Excel in a Russian locale writes semicolons, everyone else commas.
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
	// Read by header name: a column moved in Excel must not shift price into stock.
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
			log.Warnf("csv: row %d: price %q: %v", n+2, get(row, "price"), err)
			c.errs++
			continue
		}
		stock, _ := strconv.Atoi(get(row, "stock"))
		item := Item{
			SKU: get(row, "sku"), Title: get(row, "title"),
			Description: get(row, "description"), Price: price,
			Stock: max(stock, 0), Category: cellCategory(get(row, "category")),
		}
		item.Params = extraColumns(records[0], row)
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

// Columns the shop reads by meaning; every other column is a characteristic.
var kKnownColumns = map[string]bool{
	"sku": true, "title": true, "description": true, "price": true,
	"stock": true, "category": true, "images": true,
}

// Values stay text: a spreadsheet states no types and no units.
func extraColumns(header, row []string) []database.Param {
	var out []database.Param
	for i, name := range header {
		name = strings.TrimSpace(name)
		if name == "" || kKnownColumns[strings.ToLower(name)] || i >= len(row) {
			continue
		}
		if v := strings.TrimSpace(row[i]); v != "" {
			out = append(out, database.Param{Name: name, Value: v})
		}
	}
	return out
}

// Price lists spell nesting inside one cell, separated by ">", "|" or a slash.
func cellCategory(raw string) string {
	runes := []rune(raw)
	var segments []string
	start := 0
	for i, r := range runes {
		switch r {
		case '>', '|':
		case '/', '\\':
			// A slash between digits is a size, not a level: "КПБ 1,5/2 сп".
			if i > 0 && i+1 < len(runes) &&
				isDigitish(runes[i-1]) && isDigitish(runes[i+1]) {
				continue
			}
		default:
			continue
		}
		segments = append(segments, string(runes[start:i]))
		start = i + 1
	}
	return database.CategoryPath(append(segments, string(runes[start:]))...)
}

func isDigitish(r rune) bool {
	return r >= '0' && r <= '9' || r == ',' || r == '.'
}

// A price plus a note ("4.16(-9.2%)"): leading number wins, a bare second is refused.
var kPriceNote = regexp.MustCompile(`^(-?[0-9]+(?:\.[0-9]+)?)(?:\([^)]*\)|[^0-9(])*$`)

func parseMoney(s string) (int64, error) {
	s = strings.NewReplacer(" ", "", " ", "", ",", ".").Replace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		m := kPriceNote.FindStringSubmatch(s)
		if m == nil {
			return 0, fmt.Errorf("not a number")
		}
		if v, err = strconv.ParseFloat(m[1], 64); err != nil {
			return 0, fmt.Errorf("not a number")
		}
	}
	if v < 0 {
		return 0, fmt.Errorf("negative")
	}
	return int64(v*100 + 0.5), nil
}

func (c *CSV) Fetch() ([]Item, error) {
	c.parse()
	return c.rows, nil
}

// FetchErrors reports rows the file itself made unusable.
func (c *CSV) FetchErrors() int { return c.errs }
