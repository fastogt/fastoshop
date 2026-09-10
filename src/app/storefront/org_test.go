package storefront

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

func sellTo(t *testing.T, d *database.Database, kind string) {
	t.Helper()
	s, err := d.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	s.CustomerKind = kind
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
}

// A retail shop must come out of the upgrade looking exactly as it went in.
func TestPrivateShopCartFormUnchanged(t *testing.T) {
	d, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	body := c.cart(t)
	for _, marker := range []string{`name="customer"`, `name="org_name"`, `type="file"`} {
		if strings.Contains(body, marker) {
			t.Errorf("a private-only shop shows %s", marker)
		}
	}
	if s, _ := d.GetSettings(); s.CustomerKind != database.CustomerPrivate {
		t.Errorf("default customer kind is %q", s.CustomerKind)
	}
}

// The reveal is CSS, so the radio has to be a SIBLING of the block it reveals:
// nested inside its label, "~" reaches nothing and the fields stay hidden.
func TestBothShopRevealIsWiredAndShipsNoScripts(t *testing.T) {
	d, h := setup(t)
	sellTo(t, d, database.CustomerBoth)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	body := c.cart(t)
	if n := executableScripts(body); n != 0 {
		t.Fatalf("the checkout form brought %d script(s)", n)
	}
	if !strings.Contains(body, "#kind-company:checked ~ .org-fields") {
		t.Error("the reveal rule is gone from the stylesheet")
	}
	radio := strings.Index(body, `id="kind-company"`)
	fields := strings.Index(body, `class="org-fields"`)
	if radio < 0 || fields < 0 || radio > fields {
		t.Fatalf("radio at %d, fields at %d: the radio must precede them", radio, fields)
	}
	// Balanced tags before the radio mean it is not wrapped in a label, which is
	// what "~" needs: nested, the selector reaches nothing.
	before := body[:radio]
	if strings.Count(before, "<label") != strings.Count(before, "</label>") {
		t.Error("the radio sits inside a label, so it is not a sibling of .org-fields")
	}
}

func TestOrgOrderStoresNameAndUNP(t *testing.T) {
	d, h := setup(t)
	sellTo(t, d, database.CustomerBoth)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	w := c.do(t, "POST", "/cart/order", url.Values{
		"name": {"Пётр"}, "phone": {"+375291112233"}, "customer": {"company"},
		"org_name": {"ООО «Дилинс-М»"}, "org_unp": {"190304936"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("checkout: %d %s", w.Code, w.Body.String())
	}
	orders, _ := d.ListOrders()
	if len(orders) != 1 {
		t.Fatalf("orders: %d", len(orders))
	}
	if orders[0].OrgName != "ООО «Дилинс-М»" || orders[0].OrgUNP != "190304936" {
		t.Errorf("stored %q / %q", orders[0].OrgName, orders[0].OrgUNP)
	}
}

// The file is a convenience, not a gate: the seller calls back either way.
func TestOrgOrderWithoutFileIsAccepted(t *testing.T) {
	d, h := setup(t)
	sellTo(t, d, database.CustomerCompany)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	w := c.do(t, "POST", "/cart/order", url.Values{
		"name": {"Пётр"}, "email": {"p@d.by"},
		"org_name": {"ООО «Ромашка»"}, "org_unp": {"190304936"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("checkout: %d", w.Code)
	}
	orders, _ := d.ListOrders()
	if len(orders) != 1 || orders[0].RequisitesFile != "" {
		t.Fatalf("orders %d, file %q", len(orders), orders[0].RequisitesFile)
	}
}

func TestRequisitesFileLandsOutsideUploads(t *testing.T) {
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	// CreateSettings writes five columns and takes the rest from the schema
	// defaults, so the kind has to be set afterwards.
	_ = d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Лавка"})
	sellTo(t, d, database.CustomerCompany)
	_ = d.CreateProduct(&database.Product{Title: "Красный чайник", Price: 250000, Stock: 3})
	uploads := t.TempDir()
	h := New(d, "https://shop.example.com", uploads).Router()

	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"name": "Пётр", "phone": "+375291112233",
		"org_name": "ООО «Ромашка»", "org_unp": "190304936"} {
		_ = mw.WriteField(k, v)
	}
	f, _ := mw.CreateFormFile("requisites", "kartochka.pdf")
	_, _ = f.Write([]byte("%PDF-1.4 requisites"))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/cart/order", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("checkout: %d %s", w.Code, w.Body.String())
	}
	orders, _ := d.ListOrders()
	if len(orders) != 1 || orders[0].RequisitesFile == "" {
		t.Fatalf("orders %d, file %q", len(orders), orders[0].RequisitesFile)
	}
	name := orders[0].RequisitesFile
	if _, err := os.Stat(filepath.Join(RequisitesDir(uploads), name)); err != nil {
		t.Fatalf("file not stored: %v", err)
	}
	// The whole point of the separate directory: /uploads/ is a public FileServer.
	if _, err := os.Stat(filepath.Join(uploads, name)); err == nil {
		t.Error("a requisites file landed in the served uploads directory")
	}
}

// A half-named organisation is refused, and nothing typed is lost.
func TestBadUNPRefusesAndRefills(t *testing.T) {
	d, h := setup(t)
	sellTo(t, d, database.CustomerBoth)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	w := c.do(t, "POST", "/cart/order", url.Values{
		"name": {"Пётр"}, "phone": {"+375291112233"}, "customer": {"company"},
		"org_name": {"ООО «Дилинс-М»"}, "org_unp": {"19030"}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected the form back, got %d", w.Code)
	}
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatalf("an order was created from a broken tax id: %+v", orders)
	}
	body := w.Body.String()
	// The plus comes back as &#43;: html/template escapes it inside an attribute.
	for _, want := range []string{"ООО «Дилинс-М»", "375291112233", `id="kind-company" checked`} {
		if !strings.Contains(body, want) {
			t.Errorf("the form came back without %q", want)
		}
	}
}

// Marina's shop must ignore fields a crafted POST smuggles in.
func TestPrivateOnlyShopIgnoresOrgFields(t *testing.T) {
	d, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	w := c.do(t, "POST", "/cart/order", url.Values{
		"name": {"Пётр"}, "phone": {"+375291112233"}, "customer": {"company"},
		"org_name": {"ООО «Чужое»"}, "org_unp": {"190304936"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("checkout: %d", w.Code)
	}
	orders, _ := d.ListOrders()
	if len(orders) != 1 || orders[0].OrgName != "" {
		t.Fatalf("a retail shop stored an organisation: %+v", orders)
	}
}

// The oldest rule in the form applies to a company too.
func TestOrgOrderStillNeedsPhoneOrEmail(t *testing.T) {
	d, h := setup(t)
	sellTo(t, d, database.CustomerCompany)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	w := c.do(t, "POST", "/cart/order", url.Values{
		"name": {"Пётр"}, "org_name": {"ООО «Ромашка»"}, "org_unp": {"190304936"}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected the form back, got %d", w.Code)
	}
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatal("an unreachable company order was created")
	}
}

func TestOrgIDAcceptsUNPAndINN(t *testing.T) {
	ok := []string{"190304936", "AB1234567", "7707083893", "123456789012"}
	bad := []string{"ab1234567", "19030493", "1234567890123", "A11234567", "", "190 304 936"}
	for _, v := range ok {
		if !orgIDRegex.MatchString(v) {
			t.Errorf("%q must be accepted", v)
		}
	}
	for _, v := range bad {
		if orgIDRegex.MatchString(v) {
			t.Errorf("%q must be rejected", v)
		}
	}
}

func TestOrderMailBodyCarriesOrg(t *testing.T) {
	o := &database.Order{Name: "Пётр", Phone: "+375291112233",
		OrgName: "ООО «Дилинс-М»", OrgUNP: "190304936", RequisitesFile: "abc.pdf"}
	body := orderMailBody("ru", "Ерш x50 - 126.50 Br\n", "126.50 Br", o, "https://shop.by")
	for _, want := range []string{"Организация: ООО «Дилинс-М»", "УНП: 190304936", "Реквизиты"} {
		if !strings.Contains(body, want) {
			t.Errorf("owner mail is missing %q:\n%s", want, body)
		}
	}
	private := orderMailBody("ru", "", "0.00 Br", &database.Order{Name: "Иван"}, "https://shop.by")
	if strings.Contains(private, "Организация") || strings.Contains(private, "УНП") {
		t.Errorf("a private order carries organisation labels:\n%s", private)
	}
}
