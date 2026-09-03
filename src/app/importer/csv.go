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

// CSV is the shop's own template: a fixed set of columns the owner fills in a
// spreadsheet. No guessing at someone else's layout - the file's author is our
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
		"sku;title;description;price;stock;category;images;Цвет;Материал\n" +
		"CH-201;Чайник эмалированный 2 л;Объём 2 л, индукция;2500.00;7;kuhnya;" +
		"https://example.com/1.jpg|https://example.com/2.jpg;белый;эмаль\n")
}

// kCP1251 maps the upper half of windows-1251 to runes. A table beats pulling in
// x/text for one legacy charset - and this one is not optional: Russian Excel
// writes CSV in cp1251, and without it the whole catalogue arrives as mojibake.
var kCP1251 = []rune(
	"ЂЃ‚ѓ„…†‡€‰Љ‹ЊЌЋЏђ‘’“”•–-�™љ›њќћџ" +
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

// kKnownColumns are the ones the shop reads by meaning. Everything else in the
// header is a characteristic: in a spreadsheet a property is a column, and
// asking the owner to pack "Цвет=белый|Материал=эмаль" into one cell would be
// inventing a format next to the one the file already has.
var kKnownColumns = map[string]bool{
	"sku": true, "title": true, "description": true, "price": true,
	"stock": true, "category": true, "images": true,
}

// extraColumns collects the header's own columns as characteristics. A blank
// cell adds nothing: an empty property is a heading over nothing on the card.
//
// Values stay text. A spreadsheet states no types and no units, so reading a
// cell as a number would be reading the digits and hoping - and the column that
// finally proves it wrong is the one holding article numbers.
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

// cellCategory reads the category the way price lists write it. Half of them
// spell nesting inside one cell - "Текстиль > Спальня", sometimes with a slash
// or a pipe - and that is data, not a guess. A cell with no separator is a
// single level, which is what a spreadsheet usually holds.
func cellCategory(raw string) string {
	runes := []rune(raw)
	var segments []string
	start := 0
	for i, r := range runes {
		switch r {
		case '>', '|':
		case '/', '\\':
			// A slash between digits is a size, not a level: "КПБ 1,5/2 сп" is one
			// category, "Текстиль/Спальня" is two.
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

// parseMoney accepts both decimal separators and ignores spaces used as
// thousands grouping: "1 234,50" is what a spreadsheet produces.
// kPriceNote is a price followed by a note to a human: a star, a currency, a
// discount in brackets. The number is the price and the note is not, so the
// leading number is taken. A bracket may hold digits - "4.16(-9.2%)" is one
// price and its discount - but a bare second number is two prices, and
// choosing between them is not ours to do.
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
