package storefront

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
	"github.com/fastogt/fastoshop/app/mail"
)

const kCartCookie = "cart"

// ponytail: the cart lives in a cookie, capped at ~4 KB by browsers - 20 lines.
// Past that, a carts table keyed by a token stored in the cookie.
const kMaxCartLines = 20

// The cookie is buyer-editable: price, title and stock are re-read from the DB.
type cartLine struct {
	Slug string `json:"slug"`
	Qty  int    `json:"qty"`
}

func readCart(r *http.Request) []cartLine {
	c, err := r.Cookie(kCartCookie)
	if err != nil {
		return nil
	}
	raw, err := url.QueryUnescape(c.Value)
	if err != nil {
		return nil
	}
	var lines []cartLine
	if err := json.Unmarshal([]byte(raw), &lines); err != nil {
		return nil
	}
	out := make([]cartLine, 0, len(lines))
	for _, l := range lines {
		if l.Slug != "" && l.Qty > 0 && len(out) < kMaxCartLines {
			out = append(out, l)
		}
	}
	return out
}

func writeCart(w http.ResponseWriter, lines []cartLine) {
	c := &http.Cookie{Name: kCartCookie, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode}
	if len(lines) == 0 {
		c.MaxAge = -1
	} else {
		b, _ := json.Marshal(lines)
		c.Value = url.QueryEscape(string(b))
	}
	http.SetCookie(w, c)
}

func cartCount(r *http.Request) int {
	n := 0
	for _, l := range readCart(r) {
		n += l.Qty
	}
	return n
}

type cartRowVM struct {
	Slug     string
	Title    string
	ImageURL string
	Qty      int
	Stock    int
	PriceStr string
	LineStr  string
}

func (s *Storefront) resolveCart(lines []cartLine) ([]cartRowVM, int64, bool) {
	rows := make([]cartRowVM, 0, len(lines))
	var products []database.Product
	var total int64
	changed := false
	for _, l := range lines {
		p, err := s.db.GetVisibleProductBySlug(l.Slug)
		if err != nil || p.Stock <= 0 {
			changed = true
			continue
		}
		qty := l.Qty
		if qty > p.Stock {
			qty = p.Stock
			changed = true
		}
		line := p.Price * int64(qty)
		total += line
		rows = append(rows, cartRowVM{Slug: p.Slug, Title: p.Title, Qty: qty, Stock: p.Stock,
			PriceStr: priceStr(p.Price), LineStr: priceStr(line)})
		products = append(products, *p)
	}
	images, _ := s.db.ImagesFor(productIDs(products))
	for i, p := range products {
		if imgs := images[p.ID]; len(imgs) > 0 {
			rows[i].ImageURL = imageURL(imgs[0].Path)
		}
	}
	return rows, total, changed
}

func rowsToLines(rows []cartRowVM) []cartLine {
	lines := make([]cartLine, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, cartLine{Slug: r.Slug, Qty: r.Qty})
	}
	return lines
}

func (s *Storefront) Cart(w http.ResponseWriter, r *http.Request) {
	rows, total, changed := s.resolveCart(readCart(r))
	if changed {
		writeCart(w, rowsToLines(rows))
	}
	s.renderCart(w, r, rows, total, pageVM{
		Dropped: changed && len(rows) > 0,
		Goal:    takeGoal(w, r),
		Ordered: r.URL.Query().Get("ordered") == "1"})
}

// No order was created, so the buyer confirms the rest instead of seeing "thank you".
func (s *Storefront) cartSoldOut(w http.ResponseWriter, r *http.Request, slug, title string) {
	lines := make([]cartLine, 0, kMaxCartLines)
	for _, l := range readCart(r) {
		if l.Slug != slug {
			lines = append(lines, l)
		}
	}
	rows, total, _ := s.resolveCart(lines)
	writeCart(w, rowsToLines(rows))
	s.renderCart(w, r, rows, total, pageVM{SoldOut: title})
}

func (s *Storefront) renderCart(w http.ResponseWriter, r *http.Request, rows []cartRowVM, total int64, data pageVM) {
	data.Shop, data.BaseURL, data.CSS = s.shop(), s.baseURL, template.CSS(styleCSS)
	data.Cart, data.TotalStr, data.CartCount = rows, priceStr(total), countRows(rows)
	data.deliveryLine(total)
	// Robots lives in one place, or a page ends up carrying two contradicting tags.
	data.NoIndex = true
	data.fromRequest(r)
	if err := s.cart.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render cart: %v", err)
	}
}

func countRows(rows []cartRowVM) int {
	n := 0
	for _, row := range rows {
		n += row.Qty
	}
	return n
}

func (s *Storefront) CartAdd(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetVisibleProductBySlug(strings.TrimSpace(r.FormValue("slug")))
	if err != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if p.Stock <= 0 {
		http.Redirect(w, r, "/p/"+p.Slug, http.StatusSeeOther)
		return
	}
	qty, _ := strconv.Atoi(r.FormValue("qty"))
	if qty < 1 {
		qty = 1
	}
	lines := readCart(r)
	found := false
	for i := range lines {
		if lines[i].Slug == p.Slug {
			lines[i].Qty = min(lines[i].Qty+qty, p.Stock)
			found = true
			break
		}
	}
	if !found {
		if len(lines) >= kMaxCartLines {
			http.Redirect(w, r, "/cart?full=1", http.StatusSeeOther)
			return
		}
		lines = append(lines, cartLine{Slug: p.Slug, Qty: min(qty, p.Stock)})
	}
	writeCart(w, lines)
	http.Redirect(w, r, "/cart", http.StatusSeeOther)
}

func (s *Storefront) CartUpdate(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.FormValue("slug"))
	qty, _ := strconv.Atoi(r.FormValue("qty"))
	lines := readCart(r)
	out := make([]cartLine, 0, len(lines))
	for _, l := range lines {
		if l.Slug == slug {
			switch r.FormValue("action") {
			case "remove":
				continue
			case "inc":
				l.Qty++
			case "dec":
				l.Qty--
			default:
				l.Qty = qty
			}
			if l.Qty < 1 {
				continue
			}
		}
		out = append(out, l)
	}
	writeCart(w, out)
	http.Redirect(w, r, "/cart", http.StatusSeeOther)
}

func (s *Storefront) CartOrder(w http.ResponseWriter, r *http.Request) {
	// Capped before the form is parsed: the only large thing an order may carry
	// is one requisites file, and a private-only shop carries none.
	r.Body = http.MaxBytesReader(w, r.Body, kMaxRequisitesSize+(1<<20))
	shop := s.shop()
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	email := strings.TrimSpace(r.FormValue("email"))
	comment := strings.TrimSpace(r.FormValue("comment"))
	org := readOrgForm(shop, r)
	rows, total, _ := s.resolveCart(readCart(r))
	if name == "" || len(rows) == 0 {
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	typed := pageVM{FormName: name, FormComment: comment, FormPhone: phone,
		FormEmail: email, Org: org.Chosen, FormOrgName: org.Name, FormOrgUNP: org.UNP}
	// One contact is enough, but not none: an unreachable order is a lost sale.
	if phone == "" && email == "" {
		typed.NoContact = true
		s.renderCart(w, r, rows, total, typed)
		return
	}
	// The letters are a separate tick, never pre-ticked: an order is not a reason
	// to write to somebody, and consent bundled into another consent is not one.
	if strings.TrimSpace(r.FormValue("subscribe")) == "on" && email != "" {
		if err := s.db.Subscribe(email, database.SubscribeOrder, kOrderSubscribeConsent); err != nil {
			log.Warnf("subscribe from order: %v", err)
		}
	}
	// The offer and the policy are accepted by a tick, not by a line of small
	// print: this is the moment the contract is made and the data handed over.
	if r.FormValue("consent") != "on" {
		typed.NoConsent = true
		s.renderCart(w, r, rows, total, typed)
		return
	}
	// An organisation half-introduced is worse than none: the seller would have a
	// company order they cannot put on an invoice.
	if !org.Valid() {
		typed.BadOrg = true
		s.renderCart(w, r, rows, total, typed)
		return
	}
	items := make([]orderItemJSON, 0, len(rows))
	stock := make([]database.OrderItem, 0, len(rows))
	slugByID := map[int64]string{}
	var summary strings.Builder
	for _, row := range rows {
		p, err := s.db.GetVisibleProductBySlug(row.Slug)
		if err != nil {
			continue
		}
		items = append(items, orderItemJSON{
			SKU: p.SKU, Title: p.Title, Price: p.Price, Qty: row.Qty})
		stock = append(stock, database.OrderItem{ProductID: p.ID, Qty: row.Qty})
		slugByID[p.ID] = p.Slug
		fmt.Fprintf(&summary, "%s x%d - %s\n", p.Title, row.Qty, row.LineStr)
	}
	sign := shop.Sign()
	raw, _ := json.Marshal(items)
	o := &database.Order{Name: name, Phone: phone, Email: email,
		Comment: comment, ItemsJSON: string(raw), Source: sourceOf(r),
		OrgName: org.Name, OrgUNP: org.UNP, ConsentText: kOrderConsent}
	if org.Chosen {
		o.RequisitesFile = s.saveRequisites(r)
	}
	if err := s.db.CreateOrderWithStock(o, stock); err != nil {
		var oos *database.OutOfStockError
		if errors.As(err, &oos) {
			s.cartSoldOut(w, r, slugByID[oos.ProductID], oos.Name())
			return
		}
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	writeCart(w, nil)
	writeGoal(w, o.ID, total)
	s.stockChanged()
	// This email goes to the owner, so it uses the owner's language, not the product one.
	lang := shop.Lang
	body := orderMailBody(lang, summary.String(), priceStr(total)+" "+sign, o, s.baseURL)
	subject := fmt.Sprintf(i18n.T(lang, i18n.KeyNewOrderSubject), o.ID)
	// A failed email must not fail the order: async, errors only to the log.
	go func() {
		st, err := s.db.GetSettings()
		if err != nil {
			return
		}
		if err := mail.Send(st, subject, body); err != nil {
			log.Warnf("order mail: %v", err)
		}
		if email == "" {
			return
		}
		confirmation := fmt.Sprintf("%s\n\n%s%s: %s %s\n\n%s",
			fmt.Sprintf(i18n.T(lang, i18n.KeyOrderConfirmBody), o.ID),
			summary.String(), i18n.T(lang, i18n.KeyOrderTotal), priceStr(total), sign,
			s.baseURL)
		if err := mail.SendTo(st, email, fmt.Sprintf(
			i18n.T(lang, i18n.KeyOrderConfirmSubject), st.ShopName), confirmation); err != nil {
			log.Warnf("order confirmation: %v", err)
		}
	}()
	http.Redirect(w, r, "/cart?ordered=1", http.StatusSeeOther)
}

func productIDs(products []database.Product) []int64 {
	ids := make([]int64, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	return ids
}

// deliveryLine is the cart's own delivery row, built from the same numbers the
// product card publishes. The buyer is told the sum before the order, not after:
// a shop that hides it until a phone call loses the order at the cart.
func (v *pageVM) deliveryLine(total int64) {
	free := v.Shop.DeliveryFreeFrom
	switch {
	case free > 0 && total >= free:
		v.DeliveryFree = true
	case v.Shop.DeliveryCost > 0:
		v.DeliveryStr = priceStr(v.Shop.DeliveryCost)
		v.PayableStr = priceStr(total + v.Shop.DeliveryCost)
		if free > 0 {
			v.DeliveryFreeFromStr = priceStr(free)
		}
	default:
		// Nothing stated: the owner names the sum on the confirming call, and
		// saying "0" here would promise free delivery they never offered.
		v.DeliveryUnknown = true
	}
}
