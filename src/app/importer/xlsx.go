package importer

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// XLSX is a supplier's own price list, the file a seller actually has. Unlike
// the CSV template we do not get to dictate the shape here, so the columns are
// found by their headers and the header row is found by its content: a price
// list starts with a logo, a phone number and three empty rows about as often as
// it starts with the table.
//
// Reading it ourselves rather than asking for "a proper CSV" is the point: the
// owner's file opens in Excel, and telling them to rebuild it by hand is how a
// catalogue never gets imported at all.
type XLSX struct {
	Data []byte

	rows   []Item
	errs   int
	parsed bool
}

func (x *XLSX) Name() string { return "xlsx" }

func (x *XLSX) Count() (int, error) {
	x.parse()
	return len(x.rows), nil
}

func (x *XLSX) Fetch() ([]Item, error) {
	x.parse()
	return x.rows, nil
}

func (x *XLSX) FetchErrors() int { return x.errs }

// IsXLSX reports whether the bytes are a spreadsheet rather than a CSV. Every
// xlsx is a zip; a text file never starts with that signature.
func IsXLSX(raw []byte) bool { return bytes.HasPrefix(raw, []byte("PK\x03\x04")) }

// kMaxHeaderScan is how far down the sheet we look for the header row. A price
// list with more than fifty rows of preamble is not a price list.
const kMaxHeaderScan = 50

func (x *XLSX) parse() {
	if x.parsed {
		return
	}
	x.parsed = true

	z, err := zip.NewReader(bytes.NewReader(x.Data), int64(len(x.Data)))
	if err != nil {
		log.Warnf("xlsx: %v", err)
		x.errs++
		return
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}

	shared := sharedStrings(files)
	sheetName := firstSheet(files)
	if sheetName == "" {
		log.Warnf("xlsx: no worksheet inside")
		x.errs++
		return
	}
	grid, err := readSheet(files[sheetName], shared)
	if err != nil {
		log.Warnf("xlsx: %v", err)
		x.errs++
		return
	}
	photos := sheetImages(files, sheetName)

	head := headerRow(grid)
	if head < 0 {
		log.Warnf("xlsx: no header row with a title and a price column")
		x.errs++
		return
	}
	col := map[string]int{}
	for i, name := range grid[head] {
		if key := headerKey(name); key != "" {
			// The first column of a kind wins: price lists like to repeat "Цена"
			// for a second currency, and the left one is the one being sold at.
			if _, seen := col[key]; !seen {
				col[key] = i
			}
		}
	}

	for n := head + 1; n < len(grid); n++ {
		row := grid[n]
		get := func(name string) string {
			i, ok := col[name]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		title := get("title")
		if title == "" {
			continue
		}
		price, err := parseMoney(get("price"))
		if err != nil {
			log.Warnf("xlsx: row %d: price %q: %v", n+1, get("price"), err)
			x.errs++
			continue
		}
		stock, _ := strconv.Atoi(get("stock"))
		item := Item{
			SKU: get("sku"), Title: title, Description: get("description"),
			Price: price, Stock: max(stock, 0),
			Category: cellCategory(get("category")),
		}
		if item.SKU == "" {
			// Without an article a row cannot be matched on the next import, and a
			// catalogue that duplicates itself every upload is worse than one row
			// missing.
			x.errs++
			continue
		}
		for _, u := range strings.Split(get("images"), "|") {
			if u = strings.TrimSpace(u); strings.HasPrefix(u, "http") {
				item.ImageURLs = append(item.ImageURLs, u)
			}
		}
		item.ImageBlobs = photos[n]
		x.rows = append(x.rows, item)
	}
}

// headerKey maps a price list's own wording to our column names. Only the
// spellings actually seen in the wild are listed; anything unknown is ignored
// rather than guessed at, and a file whose title column is called something else
// entirely fails loudly in headerRow instead of importing nonsense.
func headerKey(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.NewReplacer("ё", "е", ".", "", ",", "").Replace(s)
	switch {
	case s == "sku" || strings.HasPrefix(s, "артикул") || strings.HasPrefix(s, "код"):
		return "sku"
	case s == "title" || strings.HasPrefix(s, "наименование") ||
		strings.HasPrefix(s, "название") || strings.HasPrefix(s, "товар"):
		return "title"
	case s == "price" || strings.HasPrefix(s, "цена") || strings.HasPrefix(s, "стоимость"):
		return "price"
	case s == "stock" || strings.HasPrefix(s, "остаток") || strings.HasPrefix(s, "наличие") ||
		strings.HasPrefix(s, "колич"):
		return "stock"
	case s == "category" || strings.HasPrefix(s, "категор") || strings.HasPrefix(s, "группа") ||
		strings.HasPrefix(s, "раздел"):
		return "category"
	case s == "description" || strings.HasPrefix(s, "описание") ||
		strings.HasPrefix(s, "характеристик"):
		return "description"
	case s == "images" || strings.HasPrefix(s, "фото") || strings.HasPrefix(s, "изображен") ||
		strings.HasPrefix(s, "картинк"):
		return "images"
	}
	return ""
}

// headerRow finds the row that names the columns. A price list starts with a
// logo and contacts, so the first row is rarely the table; what makes a row the
// header is that it names both a title and a price.
func headerRow(grid [][]string) int {
	for n := 0; n < len(grid) && n < kMaxHeaderScan; n++ {
		var title, price bool
		for _, cell := range grid[n] {
			switch headerKey(cell) {
			case "title":
				title = true
			case "price":
				price = true
			}
		}
		if title && price {
			return n
		}
	}
	return -1
}

// Sheet reading ----------------------------------------------------------

func sharedStrings(files map[string]*zip.File) []string {
	f, ok := files["xl/sharedStrings.xml"]
	if !ok {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()

	var doc struct {
		SI []struct {
			T string   `xml:"t"`
			R []string `xml:"r>t"` // a string split into styled runs
		} `xml:"si"`
	}
	if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
		return nil
	}
	out := make([]string, len(doc.SI))
	for i, si := range doc.SI {
		if si.T != "" {
			out[i] = si.T
			continue
		}
		out[i] = strings.Join(si.R, "")
	}
	return out
}

// firstSheet picks the first worksheet by name. Workbook order would be more
// correct, but a price list has one sheet that matters and it is the first one;
// resolving the workbook relationship chain to learn that is not worth it.
func firstSheet(files map[string]*zip.File) string {
	var names []string
	for name := range files {
		if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Slice(names, func(i, j int) bool {
		return sheetNumber(names[i]) < sheetNumber(names[j])
	})
	return names[0]
}

func sheetNumber(name string) int {
	digits := strings.TrimSuffix(strings.TrimPrefix(path.Base(name), "sheet"), ".xml")
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 1 << 30
	}
	return n
}

// readSheet returns the sheet as a dense grid. Cells are sparse in the file — an
// empty cell is simply absent — so they are placed by their own column letter,
// otherwise every gap would shift the rest of the row left.
func readSheet(f *zip.File, shared []string) ([][]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	var doc struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				R string `xml:"r,attr"`
				T string `xml:"t,attr"`
				V string `xml:"v"`
				S string `xml:"is>t"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
		return nil, err
	}

	var grid [][]string
	for _, row := range doc.Rows {
		n := row.R - 1
		if n < 0 {
			continue
		}
		for len(grid) <= n {
			grid = append(grid, nil)
		}
		for _, c := range row.Cells {
			i := columnIndex(c.R)
			line := grid[n]
			for len(line) <= i {
				line = append(line, "")
			}
			switch c.T {
			case "s":
				if idx, err := strconv.Atoi(c.V); err == nil && idx >= 0 && idx < len(shared) {
					line[i] = shared[idx]
				}
			case "inlineStr":
				line[i] = c.S
			default:
				line[i] = c.V
			}
			grid[n] = line
		}
	}
	return grid, nil
}

// columnIndex turns the letters of a cell reference into a zero-based column:
// "A1" is 0, "AB7" is 27.
func columnIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A') + 1
	}
	return max(n-1, 0)
}

// Embedded pictures ------------------------------------------------------

// kMaxSheetImage is the ceiling per picture. A price list holds thumbnails; a
// cell with ten megabytes in it is a mistake, and the admin upload refuses that
// size anyway.
const kMaxSheetImage = 10 << 20

// sheetImages returns the pictures anchored in the sheet, by row index. Half the
// price lists in circulation put the photo inside the cell, and asking the owner
// to extract 20 000 of them by hand is the same as saying no.
func sheetImages(files map[string]*zip.File, sheet string) map[int][][]byte {
	drawing := relTarget(files, relsFor(sheet), "drawing")
	if drawing == "" {
		return nil
	}
	media := drawingMedia(files, drawing)
	if len(media) == 0 {
		return nil
	}

	f, ok := files[drawing]
	if !ok {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()

	// Both anchor kinds carry the same thing we need: the row the picture starts
	// on and the relationship id of the image part. The blip is a nested struct
	// rather than a path: encoding/xml cannot read an attribute at the end of an
	// "a>b>c" path, and written that way the field silently stays empty.
	type blip struct {
		Embed string `xml:"embed,attr"`
	}
	type pic struct {
		Blip blip `xml:"blipFill>blip"`
	}
	type anchor struct {
		Row int `xml:"from>row"`
		Pic pic `xml:"pic"`
	}
	var doc struct {
		Two []anchor `xml:"twoCellAnchor"`
		One []anchor `xml:"oneCellAnchor"`
	}
	if err := xml.NewDecoder(rc).Decode(&doc); err != nil {
		return nil
	}

	out := map[int][][]byte{}
	for _, a := range append(doc.Two, doc.One...) {
		part, ok := media[a.Pic.Blip.Embed]
		if !ok {
			continue
		}
		blob, err := readPart(files, part)
		if err != nil {
			continue
		}
		out[a.Row] = append(out[a.Row], blob)
	}
	return out
}

func relsFor(part string) string {
	return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
}

type relationships struct {
	Rel []struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

func readRels(files map[string]*zip.File, name string) *relationships {
	f, ok := files[name]
	if !ok {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()
	var rels relationships
	if err := xml.NewDecoder(rc).Decode(&rels); err != nil {
		return nil
	}
	return &rels
}

// relTarget resolves the first relationship whose type ends with kind, as an
// archive path.
func relTarget(files map[string]*zip.File, relsName, kind string) string {
	rels := readRels(files, relsName)
	if rels == nil {
		return ""
	}
	base := path.Dir(path.Dir(relsName))
	for _, r := range rels.Rel {
		if strings.HasSuffix(r.Type, "/"+kind) {
			return path.Clean(path.Join(base, r.Target))
		}
	}
	return ""
}

// drawingMedia maps a drawing's relationship ids to the image parts they point
// at.
func drawingMedia(files map[string]*zip.File, drawing string) map[string]string {
	rels := readRels(files, relsFor(drawing))
	if rels == nil {
		return nil
	}
	base := path.Dir(path.Dir(relsFor(drawing)))
	out := map[string]string{}
	for _, r := range rels.Rel {
		if strings.HasSuffix(r.Type, "/image") {
			out[r.ID] = path.Clean(path.Join(base, r.Target))
		}
	}
	return out
}

func readPart(files map[string]*zip.File, name string) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("no such part: %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	// One byte over the cap tells "too big" from "exactly at the cap".
	blob, err := io.ReadAll(io.LimitReader(rc, kMaxSheetImage+1))
	if err != nil {
		return nil, err
	}
	if len(blob) > kMaxSheetImage {
		return nil, fmt.Errorf("larger than %d MB", kMaxSheetImage>>20)
	}
	return blob, nil
}
