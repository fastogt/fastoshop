package importer

import (
	"archive/zip"
	"bytes"
	"testing"
)

// build packs the given parts into a workbook. Writing the XML by hand is the
// point: the parser must survive what a real price list looks like, not what a
// library would have produced.
func build(t *testing.T, parts map[string]string, media map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range parts {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for name, blob := range media {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(blob); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const kStrings = `<?xml version="1.0"?><sst>
<si><t>ООО «Поставщик»</t></si>
<si><t>Код</t></si>
<si><t>Наименование</t></si>
<si><t>Цена, руб.</t></si>
<si><t>Остаток</t></si>
<si><t>Группа</t></si>
<si><t>00003104</t></si>
<si><t>Чайник эмалированный 2 л</t></si>
<si><t>Посуда &gt; Чайники</t></si>
<si><t>00003105</t></si>
<si><t>Кастрюля 3 л</t></si>
</sst>`

// A real price list opens with a title row and only then names the columns, and
// the columns are sparse: an empty cell is simply absent from the XML.
const kSheet = `<?xml version="1.0"?><worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c></row>
<row r="3"><c r="A3" t="s"><v>1</v></c><c r="B3" t="s"><v>2</v></c><c r="C3" t="s"><v>3</v></c><c r="D3" t="s"><v>4</v></c><c r="E3" t="s"><v>5</v></c></row>
<row r="4"><c r="A4" t="s"><v>6</v></c><c r="B4" t="s"><v>7</v></c><c r="C4"><v>2500.5</v></c><c r="D4"><v>7</v></c><c r="E4" t="s"><v>8</v></c></row>
<row r="5"><c r="A5" t="s"><v>9</v></c><c r="B5" t="s"><v>10</v></c><c r="C5"><v>1999</v></c></row>
</sheetData></worksheet>`

func TestXLSXReadsBySparseHeader(t *testing.T) {
	raw := build(t, map[string]string{
		"xl/sharedStrings.xml":     kStrings,
		"xl/worksheets/sheet1.xml": kSheet,
	}, nil)
	if !IsXLSX(raw) {
		t.Fatal("a workbook must be recognised by its own bytes")
	}

	x := &XLSX{Data: raw}
	items, err := x.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two products, got %d: %+v", len(items), items)
	}
	first := items[0]
	if first.SKU != "00003104" || first.Title != "Чайник эмалированный 2 л" {
		t.Fatalf("columns found by the wrong header: %+v", first)
	}
	if first.Price != 250050 {
		t.Fatalf("price in kopecks: %d", first.Price)
	}
	if first.Stock != 7 {
		t.Fatalf("stock: %d", first.Stock)
	}
	// The nesting written inside one cell is data, not a guess.
	if first.Category != "Посуда/Чайники" {
		t.Fatalf("category: %q", first.Category)
	}
	// The second row has no stock and no group cell at all, and must not shift.
	if items[1].SKU != "00003105" || items[1].Price != 199900 || items[1].Stock != 0 {
		t.Fatalf("a row with absent cells shifted: %+v", items[1])
	}
}

func TestXLSXWithoutHeaderIsRefused(t *testing.T) {
	raw := build(t, map[string]string{
		"xl/sharedStrings.xml": `<?xml version="1.0"?><sst><si><t>что-то</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet><sheetData>
			<row r="1"><c r="A1" t="s"><v>0</v></c></row></sheetData></worksheet>`,
	}, nil)
	x := &XLSX{Data: raw}
	items, _ := x.Fetch()
	if len(items) != 0 {
		t.Fatalf("a file we cannot read must import nothing: %+v", items)
	}
	if x.FetchErrors() == 0 {
		t.Fatal("and it must say so instead of reporting an empty catalogue")
	}
}

// Half the price lists in circulation put the photo inside the cell, so it has
// to come out with the row it is anchored to.
func TestXLSXTakesPicturesOutOfCells(t *testing.T) {
	raw := build(t, map[string]string{
		"xl/sharedStrings.xml":     kStrings,
		"xl/worksheets/sheet1.xml": kSheet,
		"xl/worksheets/_rels/sheet1.xml.rels": `<?xml version="1.0"?><Relationships>
			<Relationship Id="rId1"
			  Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing"
			  Target="../drawings/drawing1.xml"/></Relationships>`,
		"xl/drawings/drawing1.xml": `<?xml version="1.0"?><wsDr>
			<twoCellAnchor><from><row>3</row></from>
			  <pic><blipFill><blip embed="rId1"/></blipFill></pic></twoCellAnchor>
			</wsDr>`,
		"xl/drawings/_rels/drawing1.xml.rels": `<?xml version="1.0"?><Relationships>
			<Relationship Id="rId1"
			  Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
			  Target="../media/image1.png"/></Relationships>`,
	}, map[string][]byte{"xl/media/image1.png": kPNG})

	items, err := (&XLSX{Data: raw}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two products, got %d", len(items))
	}
	// Row 4 in the sheet is index 3 in the anchor: the first product.
	if len(items[0].ImageBlobs) != 1 || !bytes.Equal(items[0].ImageBlobs[0], kPNG) {
		t.Fatalf("the picture did not follow its row: %+v", items[0].ImageBlobs)
	}
	if len(items[1].ImageBlobs) != 0 {
		t.Fatal("a row with no picture must not borrow one")
	}
}
