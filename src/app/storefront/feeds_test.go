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

// A hidden product must leave the feeds together with the pages: a feed offer
// pointing at a 404 gets the whole feed flagged by the provider.
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
