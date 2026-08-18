import { useState } from "react";
import { useT } from "./i18n";

// Shared admin table: selection, bulk actions, server-side paging and sorting.
// Ported in spirit from the table in fastocrm (src/frontend/core/table.ts), with
// one deliberate difference: sorting is asked of the server. Sorting the loaded
// page in the browser would order a hundred rows out of twenty thousand and call
// it "sorted by price".
interface Column<T> {
  key: string;
  label: string;
  width?: string;
  // Dropped on narrow screens; the phone shows what matters, not everything.
  hideMobile?: boolean;
  sortable?: boolean;
  render?: (row: T) => React.ReactNode;
}

// Selection is what a bulk action receives. `all` means "everything the current
// filter matches", not the ticked rows — twenty thousand rows cannot be ticked,
// so that case travels to the server as a filter.
export interface Selection {
  ids: number[];
  all: boolean;
}

interface BulkAction {
  label: string;
  // Icon plus label, never an icon alone: a bare pictogram on a button that
  // changes twenty thousand rows is a guess, and this bar deletes things.
  icon?: React.ReactNode;
  danger?: boolean;
  // Actions that cannot be applied to a whole filter (deleting, for one) opt out
  // and stay disabled while "all matching" is on.
  idsOnly?: boolean;
  onClick: (sel: Selection) => void;
}

export interface Sort {
  key: string;
  desc: boolean;
}

interface Props<T> {
  columns: Column<T>[];
  rows: T[];
  // string is allowed because not every list is keyed by a number — Ozon
  // postings are identified by their posting number. Only numeric ids reach a
  // bulk action; a table keyed by strings is a read-only one.
  rowId: (row: T) => string | number;
  total: number;
  page: number;
  pageSize: number;
  onPage: (page: number) => void;
  onPageSize?: (size: number) => void;
  sort?: Sort;
  onSort?: (sort: Sort) => void;
  selectable?: boolean;
  // Off where no action can be applied to a filter — offering "select all" and
  // then greying out every button is worse than not offering it.
  allowAll?: boolean;
  bulkActions?: BulkAction[];
  emptyTitle: string;
  onRowClick?: (row: T) => void;
}

const kText = {
  selected: { ru: "Выбрано: {n}", en: "Selected: {n}" },
  selectAll: { ru: "Выбрать все {n}", en: "Select all {n}" },
  allSelected: {
    ru: "Выбраны все {n} по текущему фильтру",
    en: "All {n} matching the current filter are selected",
  },
  clear: { ru: "Сбросить", en: "Clear" },
  total: { ru: "Всего: {n}", en: "Total: {n}" },
  perPage: { ru: "{n} на странице", en: "{n} per page" },
  prev: { ru: "Назад", en: "Back" },
  next: { ru: "Вперёд", en: "Next" },
};

// kPageSizes mirrors what the CRM offers; 500 is here because a catalogue of
// twenty thousand is browsed in bulk, not read row by row.
const kPageSizes = [50, 100, 200, 500];

// pageNumbers renders at most seven buttons with ellipses, so page 214 of 397
// stays reachable without a thousand links.
function pageNumbers(page: number, pages: number): (number | "…")[] {
  const max = 7;
  if (pages <= max) return Array.from({ length: pages }, (_, i) => i + 1);
  const out: (number | "…")[] = [];
  let from = Math.max(1, page - 3);
  const to = Math.min(pages, from + max - 1);
  from = Math.max(1, to - max + 1);
  if (from > 1) {
    out.push(1);
    if (from > 2) out.push("…");
  }
  for (let i = from; i <= to; i++) out.push(i);
  if (to < pages) {
    if (to < pages - 1) out.push("…");
    out.push(pages);
  }
  return out;
}

export default function DataTable<T>({
  columns,
  rows,
  rowId,
  total,
  page,
  pageSize,
  onPage,
  onPageSize,
  sort,
  onSort,
  selectable,
  allowAll = true,
  bulkActions = [],
  emptyTitle,
  onRowClick,
}: Props<T>) {
  const t = useT(kText);
  const [picked, setPicked] = useState<Set<string | number>>(new Set());
  const [all, setAll] = useState(false);
  const pages = Math.max(1, Math.ceil(total / pageSize));

  const reset = () => {
    setPicked(new Set());
    setAll(false);
  };

  const toggle = (id: string | number) => {
    setAll(false);
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const pageIds = rows.map(rowId);
  const pageChecked =
    pageIds.length > 0 && pageIds.every((id) => picked.has(id));
  const count = all ? total : picked.size;

  const togglePage = () => {
    setAll(false);
    setPicked((prev) => {
      const next = new Set(prev);
      for (const id of pageIds) {
        if (pageChecked) next.delete(id);
        else next.add(id);
      }
      return next;
    });
  };

  const header = (c: Column<T>) => {
    if (!c.sortable || !onSort) return c.label;
    const active = sort?.key === c.key;
    return (
      <button
        className="cursor-pointer font-semibold"
        onClick={() =>
          onSort({ key: c.key, desc: active ? !sort.desc : false })
        }
      >
        {c.label}
        {active ? (sort.desc ? " ↓" : " ↑") : ""}
      </button>
    );
  };

  return (
    <div className="flex flex-col gap-3">
      {selectable && count > 0 && (
        <div className="border-brand bg-brand-soft flex flex-wrap items-center gap-3 rounded-lg border px-4 py-2">
          <span className="text-brand text-sm font-semibold">
            {all ? t("allSelected", { n: total }) : t("selected", { n: count })}
          </span>
          {allowAll && !all && picked.size > 0 && total > rows.length && (
            <button
              className="text-brand cursor-pointer text-sm underline"
              onClick={() => setAll(true)}
            >
              {t("selectAll", { n: total })}
            </button>
          )}
          {bulkActions.map((a) => (
            <button
              key={a.label}
              disabled={a.idsOnly && all}
              className={
                "cursor-pointer rounded border px-3 py-1 text-sm disabled:cursor-not-allowed disabled:opacity-40 " +
                (a.danger
                  ? "border-red-300 bg-white text-red-600"
                  : "border-line bg-white")
              }
              onClick={() =>
                a.onClick({
                  ids: [...picked].filter(
                    (x): x is number => typeof x === "number",
                  ),
                  all,
                })
              }
            >
              <span className="flex items-center gap-1.5">
                {a.icon}
                {a.label}
              </span>
            </button>
          ))}
          <button
            className="text-muted ml-auto cursor-pointer text-sm"
            onClick={reset}
          >
            {t("clear")}
          </button>
        </div>
      )}

      {rows.length === 0 ? (
        <div className="hint py-12 text-center">
          <p className="font-semibold">{emptyTitle}</p>
        </div>
      ) : (
        <div className="card overflow-x-auto p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-line text-muted border-b text-left">
                {selectable && (
                  <th className="w-10 px-3 py-3">
                    <input
                      type="checkbox"
                      checked={pageChecked}
                      onChange={togglePage}
                    />
                  </th>
                )}
                {columns.map((c) => (
                  <th
                    key={c.key}
                    style={c.width ? { width: c.width } : undefined}
                    className={
                      "px-3 py-3 font-semibold whitespace-nowrap " +
                      (c.hideMobile ? "hidden sm:table-cell" : "")
                    }
                  >
                    {header(c)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const id = rowId(row);
                const on = all || picked.has(id);
                return (
                  <tr
                    key={id}
                    className={
                      "border-line border-b last:border-0 " +
                      (on ? "bg-brand-soft " : "hover:bg-brand-soft/40 ") +
                      (onRowClick ? "cursor-pointer" : "")
                    }
                    onClick={(e) => {
                      // Clicks on a control inside the row belong to that
                      // control, not to the row.
                      const el = e.target as HTMLElement;
                      if (el.closest("input,button,a,select")) return;
                      onRowClick?.(row);
                    }}
                  >
                    {selectable && (
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          checked={on}
                          onChange={() => toggle(id)}
                        />
                      </td>
                    )}
                    {columns.map((c) => (
                      <td
                        key={c.key}
                        className={
                          "px-3 py-2 " +
                          (c.hideMobile ? "hidden sm:table-cell" : "")
                        }
                      >
                        {c.render
                          ? c.render(row)
                          : String((row as never)[c.key as never] ?? "")}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <span className="hint">{t("total", { n: total })}</span>
        {pages > 1 && (
          <div className="flex flex-wrap items-center gap-1">
            <button
              className="btn-ghost px-2 py-1 text-sm"
              disabled={page <= 1}
              onClick={() => onPage(page - 1)}
            >
              {t("prev")}
            </button>
            {pageNumbers(page, pages).map((n, i) =>
              n === "…" ? (
                <span key={`gap${i}`} className="text-muted px-1">
                  …
                </span>
              ) : (
                <button
                  key={n}
                  onClick={() => onPage(n)}
                  className={
                    "cursor-pointer rounded border px-2 py-1 text-sm " +
                    (n === page
                      ? "border-brand bg-brand text-white"
                      : "border-line bg-white")
                  }
                >
                  {n}
                </button>
              ),
            )}
            <button
              className="btn-ghost px-2 py-1 text-sm"
              disabled={page >= pages}
              onClick={() => onPage(page + 1)}
            >
              {t("next")}
            </button>
          </div>
        )}
        {onPageSize && (
          <select
            className="field ml-auto w-40"
            value={pageSize}
            onChange={(e) => onPageSize(Number(e.target.value))}
          >
            {kPageSizes.map((n) => (
              <option key={n} value={n}>
                {t("perPage", { n })}
              </option>
            ))}
          </select>
        )}
      </div>
    </div>
  );
}
