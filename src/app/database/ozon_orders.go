package database

import (
	"database/sql"
	"time"
)

// kSQLiteTime — формат, в котором SQLite отдаёт CURRENT_TIMESTAMP и в котором
// драйвер разбирает DATETIME-колонки. Пишем время только так: смешение с
// RFC3339 ломает сравнения дат прямо в SQL.
const kSQLiteTime = "2006-01-02 15:04:05"

// OzonPostingItem — строка отправления в терминах площадки: сопоставление с
// товаром магазина происходит уже внутри транзакции применения.
type OzonPostingItem struct {
	OfferID string
	Qty     int
}

// OzonPosting — отправление Ozon в том виде, в каком его применяет леддер.
// Cancelled считает вызывающий: набор «отменённых» статусов — знание о
// площадке, а не о базе.
type OzonPosting struct {
	PostingNumber string
	Status        string
	Cancelled     bool
	CreatedAt     time.Time
	Items         []OzonPostingItem
}

// OzonStatusCancelled — под ним леддер хранит любую отмену, какой бы из
// отменяющих статусов ни прислал Ozon. Иначе переход cancelled → not_accepted
// выглядел бы как новая отмена и вернул бы остаток второй раз; отдельной
// колонки «остаток возвращён» это стоить не должно.
const OzonStatusCancelled = "cancelled"

func (p *OzonPosting) storedStatus() string {
	if p.Cancelled {
		return OzonStatusCancelled
	}
	return p.Status
}

// ApplyOzonPosting применяет отправление ровно один раз и отдаёт, двигался ли
// при этом остаток.
//
// Идемпотентность держится на UNIQUE(posting_number): вставка либо создаёт
// строку (отправление новое), либо не делает ничего (уже применяли). Проверять
// SELECT-ом до вставки нельзя — два прохода синка разошлись бы в эту щель.
//
// Отмена возвращает товар на склад ровно на переходе «применяли → отменено»:
// повторное появление того же отменённого отправления в окне перекрытия
// курсора уже ничего не двигает.
func (d *Database) ApplyOzonPosting(p *OzonPosting) (moved bool, err error) {
	err = d.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO ozon_orders (posting_number, status, created_at)
			 VALUES (?, ?, ?) ON CONFLICT(posting_number) DO NOTHING`,
			p.PostingNumber, p.storedStatus(), p.CreatedAt.UTC().Format(kSQLiteTime))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			moved, err = applySeenPosting(tx, p)
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		moved, err = applyNewPosting(tx, id, p)
		return err
	})
	if err != nil {
		return false, err
	}
	return moved, nil
}

// applyNewPosting записывает позиции и списывает остаток. Отправление, которое
// мы впервые видим уже отменённым, только записывается: списывать и тут же
// возвращать один и тот же товар — лишнее движение склада на ровном месте.
func applyNewPosting(tx *sql.Tx, id int64, p *OzonPosting) (bool, error) {
	moved, oversold := false, false
	for _, it := range p.Items {
		productID, err := resolveOffer(tx, it.OfferID)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(
			`INSERT INTO ozon_order_items (ozon_order_id, product_id, offer_id, qty)
			 VALUES (?, ?, ?, ?)`, id, productID, it.OfferID, it.Qty); err != nil {
			return false, err
		}
		if productID == nil || p.Cancelled || it.Qty <= 0 {
			continue
		}
		var have int
		if err := tx.QueryRow(
			`SELECT stock FROM products WHERE id = ?`, *productID).Scan(&have); err != nil {
			return false, err
		}
		if have < it.Qty {
			oversold = true
		}
		// MAX(0, ...) — отказать в списании нельзя: площадка уже продала, и
		// отрицательный остаток на витрине хуже, чем ноль.
		if _, err := tx.Exec(
			`UPDATE products SET stock = MAX(0, stock - ?), updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`, it.Qty, *productID); err != nil {
			return false, err
		}
		moved = true
	}
	if oversold {
		if _, err := tx.Exec(`UPDATE ozon_orders SET oversold = 1 WHERE id = ?`, id); err != nil {
			return false, err
		}
	}
	return moved, nil
}

// applySeenPosting обрабатывает уже известное отправление: интересен только
// переход в отменённый статус, всё остальное — обновление статуса без движения
// склада.
func applySeenPosting(tx *sql.Tx, p *OzonPosting) (bool, error) {
	var id int64
	var status string
	if err := tx.QueryRow(
		`SELECT id, status FROM ozon_orders WHERE posting_number = ?`,
		p.PostingNumber).Scan(&id, &status); err != nil {
		return false, err
	}
	next := p.storedStatus()
	if status == next {
		return false, nil
	}
	moved := false
	// Возврат только на переходе «не отменено → отменено». Отправление,
	// увиденное отменённым сразу, склад не списывало — возвращать нечего.
	if p.Cancelled && status != OzonStatusCancelled {
		items, err := ozonOrderStock(tx, id)
		if err != nil {
			return false, err
		}
		if err := returnStock(tx, items); err != nil {
			return false, err
		}
		moved = len(items) > 0
	}
	_, err := tx.Exec(`UPDATE ozon_orders SET status = ? WHERE id = ?`, next, id)
	return moved, err
}

// ozonOrderStock читает позиции целиком до первого Exec: соединение у
// транзакции одно, держать открытый Rows во время записи нельзя.
// Несопоставленные строки выпадают — возвращать остаток некуда.
//
// ponytail: возвращаем заказанное qty, а не фактически списанное. Разойтись они
// могут только у оверселла (списали меньше, чем продали) — если это начнёт
// мешать, заводить в ozon_order_items колонку applied_qty: таблица новая,
// ALTER TABLE на живых инстансах не потребуется.
func ozonOrderStock(tx *sql.Tx, id int64) ([]OrderItem, error) {
	rows, err := tx.Query(
		`SELECT product_id, qty FROM ozon_order_items
		 WHERE ozon_order_id = ? AND product_id IS NOT NULL`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OrderItem
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ProductID, &it.Qty); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// resolveOffer ищет товар по артикулу площадки. nil — связи нет: позицию всё
// равно записываем, чтобы владелец увидел непонятую продажу, а не пустоту.
func resolveOffer(tx *sql.Tx, offerID string) (*int64, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT product_id FROM ozon_links WHERE offer_id = ? LIMIT 1`, offerID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

type OzonOrderItem struct {
	ProductID *int64
	OfferID   string
	Title     string
	Qty       int
}

type OzonOrder struct {
	ID            int64
	PostingNumber string
	Status        string
	Oversold      bool
	CreatedAt     time.Time
	Items         []OzonOrderItem
}

func (d *Database) CountOzonOrders() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM ozon_orders`).Scan(&n)
	return n, err
}

// CountOzonOrderState — счётчики для шапки вкладки: всего продаж, из них с
// оверселлом и с несопоставленными позициями.
func (d *Database) CountOzonOrderState() (total, oversold, unresolved int, err error) {
	err = d.db.QueryRow(
		`SELECT (SELECT COUNT(*) FROM ozon_orders),
		        (SELECT COUNT(*) FROM ozon_orders WHERE oversold = 1),
		        (SELECT COUNT(DISTINCT ozon_order_id) FROM ozon_order_items
		           WHERE product_id IS NULL)`).Scan(&total, &oversold, &unresolved)
	return total, oversold, unresolved, err
}

// ListOzonOrdersPage отдаёт страницу продаж с позициями. Позиции читаются
// вторым запросом по уже собранным id: JOIN размножил бы строки и сломал
// постраничность.
func (d *Database) ListOzonOrdersPage(limit, offset int) ([]OzonOrder, error) {
	rows, err := d.db.Query(
		`SELECT id, posting_number, status, oversold, created_at
		 FROM ozon_orders ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonOrder
	byID := map[int64]int{}
	for rows.Next() {
		var o OzonOrder
		if err := rows.Scan(&o.ID, &o.PostingNumber, &o.Status, &o.Oversold,
			&o.CreatedAt); err != nil {
			return nil, err
		}
		o.Items = []OzonOrderItem{}
		byID[o.ID] = len(out)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	if err := d.loadOzonOrderItems(out, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *Database) loadOzonOrderItems(orders []OzonOrder, byID map[int64]int) error {
	lo, hi := orders[len(orders)-1].ID, orders[0].ID
	if lo > hi {
		lo, hi = hi, lo
	}
	// Диапазон id вместо IN (...): страница всегда непрерывна по id внутри
	// своих границ, а склеивать плейсхолдеры руками незачем.
	rows, err := d.db.Query(
		`SELECT i.ozon_order_id, i.product_id, i.offer_id, i.qty, COALESCE(p.title, '')
		 FROM ozon_order_items i LEFT JOIN products p ON p.id = i.product_id
		 WHERE i.ozon_order_id BETWEEN ? AND ? ORDER BY i.rowid`, lo, hi)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var orderID int64
		var it OzonOrderItem
		if err := rows.Scan(&orderID, &it.ProductID, &it.OfferID, &it.Qty, &it.Title); err != nil {
			return err
		}
		if idx, ok := byID[orderID]; ok {
			orders[idx].Items = append(orders[idx].Items, it)
		}
	}
	return rows.Err()
}

// OzonOrdersSince отдаёт нулевое время, если опрос ещё ни разу не проходил —
// вызывающий сам решает, с какого окна начинать первый раз.
func (d *Database) OzonOrdersSince() (time.Time, error) {
	var t time.Time
	err := d.db.QueryRow(`SELECT orders_since FROM ozon_cursor WHERE id = 1`).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func (d *Database) SetOzonOrdersSince(t time.Time) error {
	_, err := d.db.Exec(
		`INSERT INTO ozon_cursor (id, orders_since) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET orders_since = excluded.orders_since`,
		t.UTC().Format(kSQLiteTime))
	return err
}
