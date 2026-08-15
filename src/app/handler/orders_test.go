package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/database"
)

func TestOrdersAndSettings(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Get("/api/orders", h.ListOrders)
	r.Put("/api/orders/{id}/status", h.SetOrderStatus)
	r.Get("/api/orders.csv", h.ExportOrdersCSV)
	r.Get("/api/settings", h.GetSettings)
	r.Put("/api/settings", h.UpdateSettings)

	// Orders are created by the storefront (Task 10); here — directly in the DB.
	_ = h.db.CreateOrder(&database.Order{Name: "Иван", Phone: "+7999",
		ItemsJSON: `[{"sku":"T-1","title":"Чайник","price":250000,"qty":2}]`})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/orders", nil))
	if !strings.Contains(w.Body.String(), "Иван") {
		t.Fatalf("list: %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/api/orders/1/status",
		strings.NewReader(`{"status":"done"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}

	// Invalid status — 400.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/api/orders/1/status",
		strings.NewReader(`{"status":"hacked"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d", w.Code)
	}

	// CSV with a header and one row.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/orders.csv", nil))
	csv := w.Body.String()
	if !strings.HasPrefix(csv, "id,date,name,phone,email,items,total,status") ||
		!strings.Contains(csv, "Иван") {
		t.Fatalf("csv: %s", csv)
	}

	// Settings: before setup — 404; after — secrets are masked.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/settings", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("settings before setup: %d", w.Code)
	}
	_ = h.db.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h"})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/api/settings",
		strings.NewReader(`{"shop_name":"Лавка","smtp_host":"smtp.yandex.ru","smtp_port":465,"smtp_user":"u@y.ru","smtp_password":"app-pass"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("update settings: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/settings", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Лавка") || strings.Contains(body, "app-pass") {
		t.Fatalf("secrets must not leak: %s", body)
	}
	if !strings.Contains(body, `"smtp_password_set":true`) {
		t.Fatalf("must report password presence: %s", body)
	}
}

// Broken items_json must not be printed as zero: this is a tax journal.
// A name from the public form must not be written raw: Excel runs the formula.
func TestExportOrdersCSVSafety(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Get("/api/orders.csv", h.ExportOrdersCSV)

	_ = h.db.CreateOrder(&database.Order{Name: `=cmd|'/c calc'!A1`, Phone: "+7999",
		ItemsJSON: `{ not json at all`})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/orders.csv", nil))
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("csv: %v %q", err, w.Body.String())
	}
	row := rows[1]
	if !strings.HasPrefix(row[2], "'=") {
		t.Fatalf("formula injection not neutralised: %q", row[2])
	}
	if row[5] != "ПАРСИНГ НЕ УДАЛСЯ" {
		t.Fatalf("broken items must be flagged: %q", row[5])
	}
	if row[6] != "" {
		t.Fatalf("total must be blank, not a fake number: %q", row[6])
	}
}

// An order carries a person's name and phone, so the owner must be able to
// erase it — and only by an explicit list: "delete everything the filter
// matches" is how a shop loses its journal in one click.
func TestBulkDeleteOrders(t *testing.T) {
	h := newTestHandler(t)
	_ = h.db.CreateOrder(&database.Order{Name: "Иван", Phone: "+7999",
		ItemsJSON: `[{"sku":"T-1","title":"Чайник","price":100,"qty":1}]`})
	_ = h.db.CreateOrder(&database.Order{Name: "Пётр", Email: "p@example.com",
		ItemsJSON: `[{"sku":"T-2","title":"Ковш","price":100,"qty":1}]`})
	before, err := h.db.ListOrders()
	if err != nil || len(before) == 0 {
		t.Fatalf("seed: %v %d", err, len(before))
	}

	w := httptest.NewRecorder()
	h.BulkDeleteOrders(w, httptest.NewRequest("POST", "/api/orders/bulk/delete",
		strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, before[0].ID))))
	if w.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}
	after, _ := h.db.ListOrders()
	if len(after) != len(before)-1 {
		t.Errorf("orders after delete: %d, want %d", len(after), len(before)-1)
	}

	// An empty list is a mistake, not a request to delete everything.
	w = httptest.NewRecorder()
	h.BulkDeleteOrders(w, httptest.NewRequest("POST", "/api/orders/bulk/delete",
		strings.NewReader(`{"ids":[]}`)))
	if w.Code == http.StatusOK {
		t.Error("an empty selection must not be accepted")
	}
}
