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
	s.renderCart(w, rows, total, pageVM{
		Dropped: changed && len(rows) > 0,
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
	s.renderCart(w, rows, total, pageVM{SoldOut: title})
}

func (s *Storefront) renderCart(w http.ResponseWriter, rows []cartRowVM, total int64, data pageVM) {
	data.Shop, data.BaseURL, data.CSS = s.shop(), s.baseURL, template.CSS(styleCSS)
	data.Cart, data.TotalStr, data.CartCount = rows, priceStr(total), countRows(rows)
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
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	email := strings.TrimSpace(r.FormValue("email"))
	rows, total, _ := s.resolveCart(readCart(r))
	if name == "" || len(rows) == 0 {
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	// One contact is enough, but not none: an unreachable order is a lost sale.
	if phone == "" && email == "" {
		rows, total, _ := s.resolveCart(readCart(r))
		s.renderCart(w, rows, total, pageVM{NoContact: true,
			FormName: name, FormComment: strings.TrimSpace(r.FormValue("comment"))})
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
	shop := s.shop()
	sign := shop.Sign()
	raw, _ := json.Marshal(items)
	o := &database.Order{Name: name, Phone: phone, Email: email,
		Comment: strings.TrimSpace(r.FormValue("comment")), ItemsJSON: string(raw)}
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
	s.stockChanged()
	// This email goes to the owner, so it uses the owner's language, not the product one.
	lang := shop.Lang
	body := fmt.Sprintf("%s%s: %s %s\n\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n\n%s/admin",
		summary.String(), i18n.T(lang, i18n.KeyOrderTotal), priceStr(total), sign,
		i18n.T(lang, i18n.KeyOrderName), name,
		i18n.T(lang, i18n.KeyOrderPhone), phone,
		i18n.T(lang, i18n.KeyOrderEmail), email,
		i18n.T(lang, i18n.KeyOrderComment), o.Comment, s.baseURL)
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
