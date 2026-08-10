package ozon

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// publishTest wires the tab's handlers to the same mock cabinet the sync tests
// use, so publication and pushing share one view of the platform.
func publishTest(t *testing.T, offers ...string) (*Handlers, *database.Database, *ozonMock) {
	t.Helper()
	w, d, m := newSyncTest(t)
	h := NewHandlers(d, w)
	h.BaseURL = m.URL
	m.setOffers(offers...)
	return h, d, m
}

func seedProduct(t *testing.T, d *database.Database, sku string, stock int) int64 {
	t.Helper()
	p := &database.Product{SKU: sku, Title: "Товар " + sku, Stock: stock}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// Публикация — это выбор владельца, а не всё, что совпало по артикулу.
func TestPublishOnlySelected(t *testing.T) {
	h, d, _ := publishTest(t, "A", "B")
	idA := seedProduct(t, d, "A", 5)
	seedProduct(t, d, "B", 7)

	body, _ := json.Marshal(publishRequest{ProductIDs: []int64{idA}})
	got := decode[publishResponse](t, do(t, h, "POST", "/publish", string(body)))
	if got.Published != 1 || len(got.NoCard) != 0 {
		t.Fatalf("publish: %+v", got)
	}
	links, _ := d.ListOzonLinks()
	if len(links) != 1 || links[0].ProductID != idA {
		t.Fatalf("в канал уехало лишнее: %+v", links)
	}
}

// Товар без карточки в кабинете называется по имени, а не пропадает молча.
func TestPublishReportsMissingCard(t *testing.T) {
	h, d, _ := publishTest(t, "A")
	idA := seedProduct(t, d, "A", 5)
	idNo := seedProduct(t, d, "NOPE", 1)

	body, _ := json.Marshal(publishRequest{ProductIDs: []int64{idA, idNo}})
	got := decode[publishResponse](t, do(t, h, "POST", "/publish", string(body)))
	if got.Published != 1 || len(got.NoCard) != 1 || got.NoCard[0].SKU != "NOPE" {
		t.Fatalf("publish: %+v", got)
	}
}

// Главное свойство среза: связь исчезает только после того, как на площадку
// уехал ноль. Иначе карточка остаётся торговать остатком, который мы больше
// не ведём.
func TestUnpublishZeroesStockFirst(t *testing.T) {
	h, d, m := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)

	body, _ := json.Marshal(publishRequest{ProductIDs: []int64{id}})
	do(t, h, "POST", "/publish", string(body))
	if _, _, err := h.worker.Pass(); err != nil {
		t.Fatal(err)
	}
	if got := m.lastBatch(t); len(got) != 1 || got[0].Stock != 5 {
		t.Fatalf("остаток не уехал: %+v", got)
	}

	got := decode[unpublishResponse](t, do(t, h, "POST", "/unpublish", string(body)))
	if got.Unpublished != 1 || len(got.Failed) != 0 {
		t.Fatalf("unpublish: %+v", got)
	}
	if last := m.lastBatch(t); len(last) != 1 || last[0].Stock != 0 {
		t.Fatalf("перед отвязкой не отправлен ноль: %+v", last)
	}
	if links, _ := d.ListOzonLinks(); len(links) != 0 {
		t.Fatalf("связь осталась: %+v", links)
	}
}

// Если площадка ноль не приняла, связь обязана остаться — иначе мы забудем про
// карточку, которая продолжает продавать.
func TestUnpublishKeepsLinkWhenZeroRejected(t *testing.T) {
	h, d, m := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)
	body, _ := json.Marshal(publishRequest{ProductIDs: []int64{id}})
	do(t, h, "POST", "/publish", string(body))
	if _, _, err := h.worker.Pass(); err != nil {
		t.Fatal(err)
	}

	m.failOffer("A", "склад недоступен")
	got := decode[unpublishResponse](t, do(t, h, "POST", "/unpublish", string(body)))
	if got.Unpublished != 0 || len(got.Failed) != 1 {
		t.Fatalf("unpublish: %+v", got)
	}
	if links, _ := d.ListOzonLinks(); len(links) != 1 {
		t.Fatalf("связь потеряна при неудачном обнулении: %+v", links)
	}
}

// Товар, который на площадку никогда не уезжал, снимается без вызова API.
func TestUnpublishNeverPushedNeedsNoCall(t *testing.T) {
	h, d, m := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)
	body, _ := json.Marshal(publishRequest{ProductIDs: []int64{id}})
	do(t, h, "POST", "/publish", string(body))

	before := len(m.sent())
	got := decode[unpublishResponse](t, do(t, h, "POST", "/unpublish", string(body)))
	if got.Unpublished != 1 {
		t.Fatalf("unpublish: %+v", got)
	}
	if len(m.sent()) != before {
		t.Error("лишний вызов площадки для товара, который туда не уезжал")
	}
}

// Скрытый с витрины товар остаётся кандидатом на публикацию: это разные решения.
func TestCandidatesIncludeHidden(t *testing.T) {
	h, d, _ := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)
	p, _ := d.GetProduct(id)
	p.Hidden = true
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}

	got := decode[candidatesResponse](t, do(t, h, "GET", "/candidates?page=1", ""))
	if len(got.Products) != 1 || !got.Products[0].Hidden || got.Products[0].Published {
		t.Fatalf("candidates: %+v", got.Products)
	}
	if w := do(t, h, "GET", "/candidates?q=нет", ""); w.Code != http.StatusOK {
		t.Fatalf("поиск: %d", w.Code)
	}
}
