package storefront

import (
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

func TestFeedsCatalog(t *testing.T) {
	d, h := setup(t)
	p, err := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if err != nil {
		t.Fatal(err)
	}
	// One local and one remote photo: the feed must serve absolute URLs for both.
	if err := d.AddImage(p.ID, "p1-local.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddImage(p.ID, "https://cdn.example.org/tea.jpg"); err != nil {
		t.Fatal(err)
	}
	// No category and no stock: the catch-all category and availability branches.
	sold := &database.Product{Title: "Синий чайник", SKU: "SIN-1", Price: 100000, Stock: 0}
	if err := d.CreateProduct(sold); err != nil {
		t.Fatal(err)
	}

	yml := get(t, h, "/yml.xml")
	for _, want := range []string{
		"<yml_catalog",
		"<name>Лавка</name>",
		`<currency id="RUB" rate="1"></currency>`,
		`<category id="1">kitchen</category>`,
		`<category id="2">Товары</category>`,
		"<url>https://shop.example.com/p/krasnyj-chajnik</url>",
		"<picture>https://shop.example.com/uploads/p1-local.jpg</picture>",
		"<picture>https://cdn.example.org/tea.jpg</picture>",
		"<price>2500.00</price>",
		"<currencyId>RUB</currencyId>",
		// Outside the standard, but our own importer reads it back.
		"<count>3</count>",
		`available="true"`,
		`available="false"`,
		"<categoryId>2</categoryId>",
		"<vendorCode>SIN-1</vendorCode>",
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("yml missing %q", want)
		}
	}

	gmc := get(t, h, "/gmc.xml")
	for _, want := range []string{
		`<rss version="2.0" xmlns:g="http://base.google.com/ns/1.0">`,
		"<title>Лавка</title>",
		"<link>https://shop.example.com/p/krasnyj-chajnik</link>",
		"<g:image_link>https://shop.example.com/uploads/p1-local.jpg</g:image_link>",
		"<g:additional_image_link>https://cdn.example.org/tea.jpg</g:additional_image_link>",
		"<g:price>2500.00 RUB</g:price>",
		"<g:availability>in_stock</g:availability>",
		"<g:availability>out_of_stock</g:availability>",
		"<g:condition>new</g:condition>",
		"<g:product_type>kitchen</g:product_type>",
	} {
		if !strings.Contains(gmc, want) {
			t.Errorf("gmc missing %q", want)
		}
	}
}

// A nested path becomes one element per segment tied by parentId, not a flat name.
func TestFeedsCategoryTree(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: "Кружка", SKU: "MG-1", Price: 2329, Stock: 2,
		Category: "Посуда/Посуда для напитков/Кружки, чашки"}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	yml := get(t, h, "/yml.xml")
	for _, want := range []string{
		`<category id="2">Посуда</category>`,
		`<category id="3" parentId="2">Посуда для напитков</category>`,
		`<category id="4" parentId="3">Кружки, чашки</category>`,
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("yml missing %q\n%s", want, yml)
		}
	}
	// The root of the tree carries no parent at all rather than parentId="0".
	if strings.Contains(yml, `parentId="0"`) {
		t.Error("a root category must not claim parent 0")
	}
}

func TestFeedsHideHiddenProduct(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: "Тайный чайник", Price: 1000, Stock: 5}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	p.Hidden = true
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/yml.xml", "/gmc.xml"} {
		if strings.Contains(get(t, h, path), "Тайный чайник") {
			t.Errorf("%s carries a hidden product", path)
		}
	}
}

func TestFeedsCurrencyBYN(t *testing.T) {
	d, h := setup(t)
	if err := d.UpdateSettings(&database.Settings{
		OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Лавка",
		Currency: database.ShopCurrencyBYN}); err != nil {
		t.Fatal(err)
	}
	if yml := get(t, h, "/yml.xml"); !strings.Contains(yml, "<currencyId>BYN</currencyId>") {
		t.Error("yml keeps RUB after the shop switched to BYN")
	}
	if gmc := get(t, h, "/gmc.xml"); !strings.Contains(gmc, "<g:price>2500.00 BYN</g:price>") {
		t.Error("gmc keeps RUB after the shop switched to BYN")
	}
}

// The parcel in the units the feeds state, not the ones we store.
func TestFeedsParcelAndParams(t *testing.T) {
	d, h := setup(t)
	g, l, w, ht := int64(1250), int64(300), int64(205), int64(90)
	p := &database.Product{
		Title: "Гантель", SKU: "GN-1", Price: 5000, Stock: 2,
		WeightG: &g, LengthMM: &l, WidthMM: &w, HeightMM: &ht,
		Params: []database.Param{
			{Name: "Материал", Value: "чугун"},
			{Name: "Код ТН ВЭД", Value: "9506910000"},
			{Name: "", Value: "безымянное"},
		},
	}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if err := d.SetHiddenParams([]string{"Код ТН ВЭД"}); err != nil {
		t.Fatal(err)
	}

	yml := get(t, h, "/yml.xml")
	for _, want := range []string{
		"<weight>1.25</weight>",
		"<dimensions>30/20.5/9</dimensions>",
		`<param name="Материал">чугун</param>`,
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("yml missing %q", want)
		}
	}
	// What the owner hid on the storefront stays out of the feed too.
	if strings.Contains(yml, "Код ТН ВЭД") {
		t.Error("yml carries a param the owner hid")
	}
	if strings.Contains(yml, "безымянное") {
		t.Error("yml carries a nameless param")
	}
	if gmc := get(t, h, "/gmc.xml"); !strings.Contains(gmc, "<g:shipping_weight>1.25 kg</g:shipping_weight>") {
		t.Error("gmc missing shipping weight")
	}

	// Two sides out of three is not a box: a partial value reads as a wrong one.
	p.HeightMM = nil
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(get(t, h, "/yml.xml"), "<dimensions>") {
		t.Error("yml states dimensions with a side missing")
	}
}

// Both feeds and the card's markup are asked for the brand back.
func TestFeedsBrand(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: "Чайник", SKU: "TK-9", Price: 5000, Stock: 1,
		Brand: "Гжель"}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if yml := get(t, h, "/yml.xml"); !strings.Contains(yml, "<vendor>Гжель</vendor>") {
		t.Error("yml missing <vendor>")
	}
	if gmc := get(t, h, "/gmc.xml"); !strings.Contains(gmc, "<g:brand>Гжель</g:brand>") {
		t.Error("gmc missing g:brand")
	}
	if page := get(t, h, "/p/"+p.Slug); !strings.Contains(page, `"brand": {"@type": "Brand", "name": "Гжель"}`) {
		t.Error("product page missing brand in JSON-LD")
	}
	// An empty node in a feed is worse than an absent one.
	q := &database.Product{Title: "Ковш", SKU: "KV-9", Price: 4000, Stock: 1}
	if err := d.CreateProduct(q); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(get(t, h, "/yml.xml"), "<vendor></vendor>") {
		t.Error("yml states an empty vendor")
	}
	if strings.Contains(get(t, h, "/gmc.xml"), "<g:brand></g:brand>") {
		t.Error("gmc states an empty brand")
	}
}
