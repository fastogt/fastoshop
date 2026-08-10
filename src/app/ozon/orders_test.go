package ozon

import (
	"fmt"
	"testing"
	"time"

	"github.com/fastogt/fastoshop/app/database"
)

func TestPollAppliesPostingOnce(t *testing.T) {
	w, d, m := newSyncTest(t)
	a := seedLinked(t, d, "A", 10)
	b := seedLinked(t, d, "B", 4)
	m.setPostings(posting("0001-1", "awaiting_deliver", line("A", 3), line("B", 1)))

	pass(t, w)
	if got := stockOf(t, d, a); got != 7 {
		t.Fatalf("A: ожидали 7, получили %d", got)
	}
	if got := stockOf(t, d, b); got != 3 {
		t.Fatalf("B: ожидали 3, получили %d", got)
	}

	// Второй раз то же отправление — леддер отбивает его по UNIQUE.
	pass(t, w)
	if stockOf(t, d, a) != 7 || stockOf(t, d, b) != 3 {
		t.Fatalf("повторное отправление сдвинуло остатки: A=%d B=%d",
			stockOf(t, d, a), stockOf(t, d, b))
	}
	total, oversold, unresolved, err := d.CountOzonOrderState()
	if err != nil || total != 1 || oversold != 0 || unresolved != 0 {
		t.Fatalf("счётчики: %v %d/%d/%d", err, total, oversold, unresolved)
	}
}

func TestPollOversellClampsAtZero(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 2)
	m.setPostings(posting("0002-1", "awaiting_packaging", line("A", 5)))

	pass(t, w)
	if got := stockOf(t, d, id); got != 0 {
		t.Fatalf("остаток ушёл в минус или не списался: %d", got)
	}
	orders, err := d.ListOzonOrdersPage(50, 0)
	if err != nil || len(orders) != 1 {
		t.Fatalf("заказ не записан: %v %+v", err, orders)
	}
	if !orders[0].Oversold {
		t.Fatalf("оверселл не отмечен: %+v", orders[0])
	}
	t.Logf("оверселл: %s qty=%d остаток=%d",
		orders[0].PostingNumber, orders[0].Items[0].Qty, stockOf(t, d, id))
}

func TestPollUnresolvedOfferIsSurfaced(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	m.setPostings(posting("0003-1", "awaiting_deliver", line("A", 1), line("НЕТ-ТАКОГО", 2)))

	pass(t, w)
	if got := stockOf(t, d, id); got != 4 {
		t.Fatalf("связанная позиция не списалась: %d", got)
	}
	orders, err := d.ListOzonOrdersPage(50, 0)
	if err != nil || len(orders) != 1 || len(orders[0].Items) != 2 {
		t.Fatalf("позиции: %v %+v", err, orders)
	}
	var unresolved *database.OzonOrderItem
	for i := range orders[0].Items {
		if orders[0].Items[i].ProductID == nil {
			unresolved = &orders[0].Items[i]
		}
	}
	if unresolved == nil || unresolved.OfferID != "НЕТ-ТАКОГО" || unresolved.Qty != 2 {
		t.Fatalf("несопоставленная позиция потерялась: %+v", orders[0].Items)
	}
	_, _, cnt, err := d.CountOzonOrderState()
	if err != nil || cnt != 1 {
		t.Fatalf("счётчик несопоставленных: %v %d", err, cnt)
	}
}

func TestPollCancellationRestocksOnce(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	m.setPostings(posting("0004-1", "awaiting_deliver", line("A", 2)))
	pass(t, w)
	if got := stockOf(t, d, id); got != 3 {
		t.Fatalf("продажа не списалась: %d", got)
	}

	m.setPostings(posting("0004-1", "cancelled", line("A", 2)))
	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("отмена не вернула товар: %d", got)
	}

	// Ещё две встречи той же отмены (в том числе под другим отменяющим
	// статусом) остаток больше не двигают.
	pass(t, w)
	m.setPostings(posting("0004-1", "not_accepted", line("A", 2)))
	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("повторная отмена вернула товар второй раз: %d", got)
	}
}

func TestPollCancelledOnFirstSightDoesNotMoveStock(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	m.setPostings(posting("0005-1", "cancelled", line("A", 2)))

	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("сразу отменённое отправление сдвинуло остаток: %d", got)
	}
	orders, err := d.ListOzonOrdersPage(50, 0)
	if err != nil || len(orders) != 1 || orders[0].Status != database.OzonStatusCancelled {
		t.Fatalf("отмена не записана: %v %+v", err, orders)
	}

	// И следующая встреча тоже ничего не возвращает.
	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("отменённое отправление вернуло несписанный товар: %d", got)
	}
}

func TestPollCursorAdvancesOnlyOnSuccess(t *testing.T) {
	w, d, m := newSyncTest(t)
	seedLinked(t, d, "A", 5)

	// Ответ неизвестного формата — курсор трогать нельзя, иначе окно с
	// продажами уедет навсегда.
	m.mu.Lock()
	m.postingsRaw = `{"result":{"postings":[{"status":"awaiting_deliver"}]}}`
	m.mu.Unlock()
	if _, _, err := w.Pass(); err == nil {
		t.Fatal("неизвестный формат ответа должен быть ошибкой, а не тихим нулём")
	}
	t.Logf("ошибка опроса видна во вкладке: %s", w.PollError())
	if since, err := d.OzonOrdersSince(); err != nil || !since.IsZero() {
		t.Fatalf("курсор сдвинулся после неудачи: %v %s", err, since)
	}

	m.mu.Lock()
	m.postingsRaw = ""
	m.mu.Unlock()
	before := time.Now().UTC().Add(-time.Second)
	m.setPostings(posting("0006-1", "awaiting_deliver", line("A", 1)))
	pass(t, w)
	since, err := d.OzonOrdersSince()
	if err != nil || since.Before(before) {
		t.Fatalf("курсор не сдвинулся: %v %s", err, since)
	}
	if w.PollError() != "" {
		t.Fatalf("ошибка не снялась: %s", w.PollError())
	}
	t.Logf("курсор: %s", since.Format(time.RFC3339))

	// Следующий опрос уходит с нахлёстом назад — и переспрошенное отправление
	// проходит вхолостую.
	pass(t, w)
	fs := m.filters()
	last, err := time.Parse(time.RFC3339, fs[len(fs)-1].Since)
	if err != nil {
		t.Fatal(err)
	}
	overlap := since.Sub(last)
	t.Logf("нахлёст окна: %s", overlap)
	if overlap < kPollOverlap-time.Second || overlap > kPollOverlap+time.Second {
		t.Fatalf("нахлёст %s не равен %s", overlap, kPollOverlap)
	}
	if n, _ := d.CountOzonOrders(); n != 1 {
		t.Fatalf("нахлёст создал дубль: %d", n)
	}
}

// TestFullCycle — срез целиком: связали, отправили уровень, площадка продала,
// опрос применил продажу, следующий проход отправил уменьшенный уровень.
func TestFullCycle(t *testing.T) {
	h, d := newTestHandlers(t)
	m := newOzonMock(t)
	h.BaseURL, h.worker.BaseURL = m.URL, m.URL
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "77", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	id := seedLinked(t, d, "A", 10)

	step := func(format string, args ...any) { t.Logf("· "+format, args...) }

	got := decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	step("первая отправка: pushed=%d, уровень на площадке %d",
		got.Pushed, m.lastBatch(t)[0].Stock)
	if got.Pushed != 1 || m.lastBatch(t)[0].Stock != 10 {
		t.Fatalf("первая отправка: %+v %+v", got, m.lastBatch(t))
	}

	m.setPostings(posting("0007-1", "awaiting_deliver", line("A", 4)))
	got = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	step("после продажи 4 шт: наш остаток %d, отправлено %d, уровень на площадке %d",
		stockOf(t, d, id), got.Pushed, m.lastBatch(t)[0].Stock)
	if stockOf(t, d, id) != 6 || got.Pushed != 1 || m.lastBatch(t)[0].Stock != 6 {
		t.Fatalf("цикл не замкнулся: остаток=%d %+v %+v",
			stockOf(t, d, id), got, m.lastBatch(t))
	}

	s := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	step("вкладка: продаж %d, оверселл %d, несопоставленных %d, ошибка опроса %q",
		s.OrdersTotal, s.OrdersOversold, s.OrdersUnresolved, s.PollError)
	if s.OrdersTotal != 1 || s.OrdersOversold != 0 || s.PollError != "" {
		t.Fatalf("статус вкладки: %+v", s)
	}

	list := decode[ozonOrdersResponse](t, do(t, h, "GET", "/orders?page=1", ""))
	step("журнал: %s (%s), позиция %q x%d",
		list.Orders[0].PostingNumber, list.Orders[0].Status,
		list.Orders[0].Items[0].Title, list.Orders[0].Items[0].Qty)
	if list.Total != 1 || len(list.Orders) != 1 ||
		list.Orders[0].Items[0].Title != "Товар A" {
		t.Fatalf("журнал продаж: %+v", list)
	}
}

func TestOrdersEndpointPaginates(t *testing.T) {
	h, d := newTestHandlers(t)
	for i := range kOrdersPageSize + 3 {
		if _, err := d.ApplyOzonPosting(&database.OzonPosting{
			PostingNumber: fmt.Sprintf("P-%03d", i),
			Status:        "awaiting_deliver",
			CreatedAt:     time.Now().Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	first := decode[ozonOrdersResponse](t, do(t, h, "GET", "/orders", ""))
	if first.Total != kOrdersPageSize+3 || len(first.Orders) != kOrdersPageSize {
		t.Fatalf("первая страница: total=%d len=%d", first.Total, len(first.Orders))
	}
	second := decode[ozonOrdersResponse](t, do(t, h, "GET", "/orders?page=2", ""))
	if len(second.Orders) != 3 || second.Page != 2 {
		t.Fatalf("вторая страница: %+v", second)
	}
	// Новые сверху: первое на второй странице старее последнего на первой.
	if second.Orders[0].CreatedAt.After(first.Orders[len(first.Orders)-1].CreatedAt) {
		t.Fatalf("порядок страниц сломан: %s vs %s",
			second.Orders[0].CreatedAt, first.Orders[len(first.Orders)-1].CreatedAt)
	}
}
