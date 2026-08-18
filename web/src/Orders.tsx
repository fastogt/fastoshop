import { useCallback, useEffect, useState } from "react";
import { api, apiError, type Order } from "./api";
import Modal from "./Modal";
import { useLang, useT } from "./i18n";
import { toRubles } from "./money";
import DataTable, { type Sort } from "./DataTable";
import { IconCheck, IconTrash, IconUndo, IconX } from "./Icons";
import { useSign } from "./shop";

const kStatusTitles = {
  new: { ru: "Новый", en: "New" },
  done: { ru: "Выполнен", en: "Completed" },
  cancelled: { ru: "Отменён", en: "Cancelled" },
};

const kText = {
  ...kStatusTitles,
  heading: { ru: "Заказы", en: "Orders" },
  exportCsv: {
    ru: "Экспорт CSV для бухгалтера",
    en: "Export CSV for the accountant",
  },
  empty: {
    ru: "Заказов пока нет. Они появятся здесь сразу после оформления на витрине — и продублируются письмом, если настроена почта.",
    en: "No orders yet. They show up here as soon as someone checks out in the shop — and by email too, once mail is configured.",
  },
  thNumber: { ru: "#", en: "#" },
  itemsCount: { ru: "{n} позиции · {q} шт", en: "{n} item(s) · {q} pcs" },
  broken: { ru: "Состав не читается", en: "Contents unreadable" },
  cardTitle: { ru: "Заказ №{n}", en: "Order #{n}" },
  cardSku: { ru: "Артикул", en: "SKU" },
  cardItem: { ru: "Товар", en: "Product" },
  cardPrice: { ru: "Цена", en: "Price" },
  cardQty: { ru: "Кол-во", en: "Qty" },
  cardSum: { ru: "Сумма", en: "Sum" },
  cardComment: { ru: "Комментарий покупателя", en: "Buyer's comment" },
  cardClose: { ru: "Закрыть", en: "Close" },
  thDate: { ru: "Дата", en: "Date" },
  thCustomer: { ru: "Покупатель", en: "Customer" },
  thItems: { ru: "Состав", en: "Items" },
  thTotal: { ru: "Сумма", en: "Total" },
  thStatus: { ru: "Статус", en: "Status" },
  statusFailed: {
    ru: "Не удалось изменить статус",
    en: "Could not change the status",
  },
  markDone: { ru: "Отметить выполненными", en: "Mark as completed" },
  bulkDelete: { ru: "Удалить", en: "Delete" },
  confirmDelete: {
    ru: "Удалить заказы ({n})? Данные покупателя и состав заказа исчезнут навсегда. Остатки не вернутся — для этого отмените заказ.",
    en: "Delete {n} order(s)? The buyer's details and the contents disappear for good. Stock is not returned — cancel the order for that.",
  },
  deleted: { ru: "Удалено: {n}", en: "Deleted: {n}" },
  markCancelled: { ru: "Отменить", en: "Cancel" },
  markNew: { ru: "Вернуть в работу", en: "Reopen" },
  bulkDone: { ru: "Изменено заказов: {n}", en: "Orders changed: {n}" },
  bulkPartly: {
    ru: "Изменено: {n}. Не удалось: {failed} — вернуть в работу можно только то, что ещё есть на складе.",
    en: "Changed: {n}. Failed: {failed} — an order can only be reopened while the goods are still in stock.",
  },
};

// Reopening an order can hit the stock limit: the server refuses, and the shop
// owner must see why instead of a select that silently rolls back.
// gofastogt wraps the text in "invalid input (…)" — show only the substance.
const statusError = (err: unknown, fallback: string) =>
  apiError(err)?.match(/\((.*)\)$/)?.[1] ?? fallback;

export default function Orders() {
  const [list, setList] = useState<Order[]>([]);
  const [total_, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [per, setPer] = useState(50);
  const [sort, setSort] = useState<Sort>({ key: "created", desc: true });
  const [bulkMsg, setBulkMsg] = useState("");
  const t = useT(kText);
  const sign = useSign();
  const lang = useLang();

  const reload = useCallback(
    () =>
      api.orders(page, per, sort.key, sort.desc ? "desc" : "asc").then((r) => {
        setList(r.orders ?? []);
        setTotal(r.total || 0);
      }),
    [page, per, sort],
  );
  useEffect(() => {
    reload();
  }, [reload]);

  // Status changes move stock, so each order goes in its own transaction: a
  // failure on one must not roll back the others and must not pass silently.
  const remove = async (ids: number[]) => {
    if (!window.confirm(t("confirmDelete", { n: ids.length }))) return;
    try {
      const r = await api.bulkDeleteOrders(ids);
      setBulkMsg(t("deleted", { n: r.deleted }));
      await reload();
    } catch {
      setBulkMsg(t("statusFailed"));
    }
  };

  const bulkStatus = async (ids: number[], status: string) => {
    setBulkMsg("");
    try {
      const r = await api.bulkOrderStatus(ids, status);
      setBulkMsg(
        r.failed.length
          ? t("bulkPartly", { n: r.updated, failed: r.failed.length })
          : t("bulkDone", { n: r.updated }),
      );
      await reload();
    } catch {
      setBulkMsg(t("statusFailed"));
    }
  };

  const [card, setCard] = useState<Order | null>(null);

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold">{t("heading")}</h1>
        <a href="/api/orders.csv" className="btn-ghost">
          {t("exportCsv")}
        </a>
      </div>

      <DataTable<Order>
        columns={[
          {
            key: "id",
            label: t("thNumber"),
            width: "60px",
            render: (o) => <span className="text-muted">{o.id}</span>,
          },
          {
            key: "created",
            label: t("thDate"),
            sortable: true,
            render: (o) => (
              <span className="whitespace-nowrap">
                {new Date(o.created_at).toLocaleString(lang, {
                  day: "2-digit",
                  month: "2-digit",
                  hour: "2-digit",
                  minute: "2-digit",
                })}
              </span>
            ),
          },
          {
            key: "name",
            label: t("thCustomer"),
            sortable: true,
            width: "220px",
            render: (o) => (
              <>
                <div className="font-medium">{o.name}</div>
                {o.phone && (
                  <a
                    className="text-brand block whitespace-nowrap"
                    href={`tel:${o.phone}`}
                  >
                    {o.phone}
                  </a>
                )}
                {o.email && (
                  <a className="text-brand block" href={`mailto:${o.email}`}>
                    {o.email}
                  </a>
                )}
                {o.comment && (
                  <div className="hint mt-1 line-clamp-1">💬 {o.comment}</div>
                )}
              </>
            ),
          },
          {
            key: "items",
            label: t("thItems"),
            // Two lines at most: a supplier's title runs to two hundred
            // characters and would otherwise set the height of the whole row.
            render: (o) =>
              o.broken ? (
                <span className="text-red-600">{t("broken")}</span>
              ) : (
                <>
                  <div className="line-clamp-2">{o.items[0]?.title}</div>
                  <div className="hint">
                    {t("itemsCount", {
                      n: o.items.length,
                      q: o.items.reduce((acc, i) => acc + i.qty, 0),
                    })}
                  </div>
                </>
              ),
          },
          {
            key: "total",
            label: t("thTotal"),
            width: "110px",
            render: (o) => (
              <span className="font-semibold whitespace-nowrap">
                {o.broken ? "—" : `${toRubles(o.total)} ${sign}`}
              </span>
            ),
          },
          {
            key: "status",
            label: t("thStatus"),
            sortable: true,
            width: "150px",
            render: (o) => (
              <select
                className="field py-1"
                value={o.status}
                onChange={(e) =>
                  api
                    .setOrderStatus(o.id, e.target.value)
                    .catch((err) => alert(statusError(err, t("statusFailed"))))
                    .finally(reload)
                }
              >
                {(
                  Object.keys(kStatusTitles) as (keyof typeof kStatusTitles)[]
                ).map((v) => (
                  <option key={v} value={v}>
                    {t(v)}
                  </option>
                ))}
              </select>
            ),
          },
        ]}
        rows={list}
        rowId={(o) => o.id}
        onRowClick={setCard}
        total={total_}
        page={page}
        pageSize={per}
        onPage={setPage}
        onPageSize={(n) => {
          setPer(n);
          setPage(1);
        }}
        sort={sort}
        onSort={(next) => {
          setSort(next);
          setPage(1);
        }}
        selectable
        // No order action applies to "everything by filter": status changes
        // move stock, and that kind of sweep is not a working scenario.
        allowAll={false}
        bulkActions={[
          {
            label: t("markDone"),
            icon: <IconCheck />,
            idsOnly: true,
            onClick: (sel) => bulkStatus(sel.ids, "done"),
          },
          {
            label: t("markNew"),
            icon: <IconUndo />,
            idsOnly: true,
            onClick: (sel) => bulkStatus(sel.ids, "new"),
          },
          {
            label: t("bulkDelete"),
            icon: <IconTrash />,
            danger: true,
            idsOnly: true,
            onClick: (sel) => void remove(sel.ids),
          },
          {
            label: t("markCancelled"),
            icon: <IconX />,
            danger: true,
            idsOnly: true,
            onClick: (sel) => bulkStatus(sel.ids, "cancelled"),
          },
        ]}
        emptyTitle={t("empty")}
      />

      {bulkMsg && <p className="text-green-700">{bulkMsg}</p>}

      {/* The card is where the owner works the order before calling: the whole
          composition with articles and unit prices, and the comment in full. */}
      {card && (
        <Modal
          title={t("cardTitle", { n: card.id })}
          onClose={() => setCard(null)}
        >
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap gap-x-6 gap-y-1">
              <span className="text-muted">
                {new Date(card.created_at).toLocaleString(lang)}
              </span>
              <span className="font-medium">{card.name}</span>
              {card.phone && (
                <a
                  className="text-brand whitespace-nowrap"
                  href={`tel:${card.phone}`}
                >
                  {card.phone}
                </a>
              )}
              {card.email && (
                <a className="text-brand" href={`mailto:${card.email}`}>
                  {card.email}
                </a>
              )}
            </div>

            {card.comment && (
              <div>
                <div className="label">{t("cardComment")}</div>
                <p className="whitespace-pre-line">{card.comment}</p>
              </div>
            )}

            <table className="w-full text-sm">
              <thead className="text-muted text-left">
                <tr>
                  <th className="py-1 pr-3 font-normal">{t("cardSku")}</th>
                  <th className="py-1 pr-3 font-normal">{t("cardItem")}</th>
                  <th className="py-1 pr-3 text-right font-normal">
                    {t("cardPrice")}
                  </th>
                  <th className="py-1 pr-3 text-right font-normal">
                    {t("cardQty")}
                  </th>
                  <th className="py-1 text-right font-normal">
                    {t("cardSum")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {card.items.map((it, i) => (
                  <tr key={`${it.sku}-${i}`} className="border-line border-t">
                    <td className="py-2 pr-3 whitespace-nowrap">{it.sku}</td>
                    <td className="py-2 pr-3">{it.title}</td>
                    <td className="py-2 pr-3 text-right whitespace-nowrap">
                      {toRubles(it.price)} {sign}
                    </td>
                    <td className="py-2 pr-3 text-right">{it.qty}</td>
                    <td className="py-2 text-right whitespace-nowrap">
                      {toRubles(it.price * it.qty)} {sign}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr className="border-line border-t font-semibold">
                  <td className="py-2" colSpan={4}>
                    {t("thTotal")}
                  </td>
                  <td className="py-2 text-right whitespace-nowrap">
                    {toRubles(card.total)} {sign}
                  </td>
                </tr>
              </tfoot>
            </table>
          </div>
        </Modal>
      )}
    </div>
  );
}
