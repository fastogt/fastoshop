import { useCallback, useEffect, useState } from "react";
import { api, apiError, type Order } from "./api";
import { useLang, useT } from "./i18n";
import DataTable, { type Sort } from "./DataTable";
import { IconCheck, IconUndo, IconX } from "./Icons";
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

  const items = (o: Order) => {
    try {
      return (JSON.parse(o.items_json) as { title: string; qty: number }[])
        .map((i) => `${i.title} ×${i.qty}`)
        .join(", ");
    } catch {
      return o.items_json;
    }
  };

  const total = (o: Order) => {
    try {
      const sum = (
        JSON.parse(o.items_json) as { price: number; qty: number }[]
      ).reduce((acc, i) => acc + i.price * i.qty, 0);
      return `${(sum / 100).toFixed(2)} ${sign}`;
    } catch {
      return "—";
    }
  };

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
            render: (o) => (
              <>
                <div className="font-medium">{o.name}</div>
                <a className="text-brand" href={`tel:${o.phone}`}>
                  {o.phone}
                </a>
                {o.comment && <div className="hint mt-1">{o.comment}</div>}
              </>
            ),
          },
          { key: "items", label: t("thItems"), render: items },
          {
            key: "total",
            label: t("thTotal"),
            render: (o) => (
              <span className="font-semibold whitespace-nowrap">
                {total(o)}
              </span>
            ),
          },
          {
            key: "status",
            label: t("thStatus"),
            sortable: true,
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
    </div>
  );
}
