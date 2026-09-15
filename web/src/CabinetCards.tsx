import { useCallback, useEffect, useState, type ReactNode } from "react";
import { api, type CabinetCardBase, type CardsView, type Product } from "./api";
import DataTable from "./DataTable";
import { useFeedback } from "./feedback";
import { useT } from "./i18n";

const kText = {
  filterAll: { ru: "Все", en: "All" },
  filterUnlinked: { ru: "Не связанные", en: "Not linked" },
  filterPacks: { ru: "Наборы", en: "Packs" },
  search: {
    ru: "Поиск по артикулу или названию",
    en: "Search by article or title",
  },
  colCard: { ru: "Карточка {platform}", en: "{platform} card" },
  colProduct: { ru: "Товар магазина", en: "Shop product" },
  colQty: { ru: "шт", en: "units" },
  colOut: { ru: "→ на {platform}", en: "→ to {platform}" },
  colPrice: { ru: "Цена на {platform}", en: "Price on {platform}" },
  fromArticle: { ru: "из артикула", en: "from article" },
  pickProduct: { ru: "выбрать товар", en: "pick a product" },
  link: { ru: "Связать", en: "Link" },
  unlink: { ru: "Отвязать", en: "Unlink" },
  empty: { ru: "Карточек нет", en: "No cards" },
  error: { ru: "не удалось", en: "failed" },
  shared: {
    ru: "{title} ({stock} шт), карточек {platform}: {n} ({packs}). Все делят один остаток.",
    en: "{title} ({stock} units), {platform} cards: {n} ({packs}). They share one stock.",
  },
};

type Picked = Pick<Product, "id" | "title" | "sku" | "stock">;

function ProductPicker({
  value,
  onPick,
  placeholder,
}: {
  value: Picked | null;
  onPick: (p: Picked) => void;
  placeholder: string;
}) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [found, setFound] = useState<Picked[]>([]);

  useEffect(() => {
    if (!open || !q.trim()) return;
    const id = setTimeout(() => {
      void api.products(1, q, undefined, 10).then((r) => setFound(r.products));
    }, 300);
    return () => clearTimeout(id);
  }, [q, open]);

  return (
    <div className="relative">
      <input
        className="field w-56"
        placeholder={value ? `${value.title} · ${value.stock}` : placeholder}
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        // Delayed so a click on an option lands before the list disappears.
        onBlur={() => setTimeout(() => setOpen(false), 150)}
      />
      {open && q.trim() && found.length > 0 && (
        <ul className="border-line absolute z-10 mt-1 max-h-64 w-72 overflow-auto rounded border bg-white shadow">
          {found.map((p) => (
            <li key={p.id}>
              <button
                type="button"
                className="flex w-full gap-2 px-2 py-1 text-left text-sm hover:bg-gray-100"
                onMouseDown={() => {
                  onPick(p);
                  setQ("");
                  setOpen(false);
                }}
              >
                <span className="flex-1">{p.title}</span>
                <span className="text-muted">{p.sku}</span>
                <span>{p.stock}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

interface Draft {
  product: Picked | null;
  qty: string;
}

// Cards of one cabinet, each tied by hand to a shop product and a pack size.
export default function CabinetCards<T extends CabinetCardBase>({
  platform,
  load,
  cardKey,
  cardLabel,
  link,
  unlink,
  price,
  onChanged,
}: {
  platform: string;
  load: (
    page: number,
    q: string,
    view: CardsView,
  ) => Promise<{ cards: T[]; total: number; page_size: number }>;
  cardKey: (c: T) => string;
  cardLabel: (c: T) => ReactNode;
  link: (c: T, productId: number, qty: number) => Promise<unknown>;
  unlink: (linkId: number) => Promise<unknown>;
  price: (minor: number) => string;
  onChanged: () => void;
}) {
  const t = useT(kText);
  const { fail, line } = useFeedback(kText.error);
  const [view, setView] = useState<CardsView>("unlinked");
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [cards, setCards] = useState<T[]>([]);
  const [total, setTotal] = useState(0);
  const [pageSize, setPageSize] = useState(100);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");

  const reload = useCallback(async () => {
    try {
      const r = await load(page, query, view);
      setCards(r.cards);
      setTotal(r.total);
      setPageSize(r.page_size);
    } catch (e) {
      setMsg(fail(e));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fail is rebuilt every render
  }, [load, page, query, view]);

  useEffect(() => void reload(), [reload]);

  useEffect(() => {
    const id = setTimeout(() => {
      setQuery(search);
      setPage(1);
    }, 300);
    return () => clearTimeout(id);
  }, [search]);

  const draftOf = (c: T): Draft =>
    drafts[cardKey(c)] ?? { product: null, qty: String(c.suggested_qty) };

  const act = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setMsg("");
    try {
      await action();
      await reload();
      onChanged();
    } catch (e) {
      setMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  // Several cards of one product draw on one stock: say so under the table.
  const groups = new Map<number, T[]>();
  for (const c of cards) {
    if (c.link_id)
      groups.set(c.product_id, [...(groups.get(c.product_id) ?? []), c]);
  }
  const shared = [...groups.values()].filter((g) => g.length > 1);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        {(
          [
            ["unlinked", t("filterUnlinked")],
            ["packs", t("filterPacks")],
            ["all", t("filterAll")],
          ] as const
        ).map(([kind, label]) => (
          <button
            key={kind}
            onClick={() => {
              setView(kind);
              setPage(1);
            }}
            className={
              "rounded-full border px-3 py-1 text-sm " +
              (view === kind
                ? "border-brand text-brand font-semibold"
                : "border-line text-muted")
            }
          >
            {label}
          </button>
        ))}
        <input
          className="field w-64"
          placeholder={t("search")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      <DataTable<T>
        columns={[
          {
            key: "card",
            label: t("colCard", { platform }),
            render: cardLabel,
          },
          {
            key: "product",
            label: t("colProduct"),
            render: (c) =>
              c.link_id ? (
                <>
                  {c.product_title}
                  {c.product_sku && (
                    <span className="text-muted"> · {c.product_sku}</span>
                  )}
                </>
              ) : (
                <ProductPicker
                  value={draftOf(c).product}
                  placeholder={t("pickProduct")}
                  onPick={(p) =>
                    setDrafts({
                      ...drafts,
                      [cardKey(c)]: { ...draftOf(c), product: p },
                    })
                  }
                />
              ),
          },
          {
            key: "qty",
            label: t("colQty"),
            render: (c) =>
              c.link_id ? (
                c.qty
              ) : (
                <div>
                  <input
                    className="field w-16"
                    inputMode="numeric"
                    value={draftOf(c).qty}
                    onChange={(e) =>
                      setDrafts({
                        ...drafts,
                        [cardKey(c)]: { ...draftOf(c), qty: e.target.value },
                      })
                    }
                  />
                  {c.suggested_qty > 1 &&
                    draftOf(c).qty === String(c.suggested_qty) && (
                      <div className="text-muted text-xs">
                        {t("fromArticle")}
                      </div>
                    )}
                </div>
              ),
          },
          {
            key: "out",
            label: t("colOut", { platform }),
            render: (c) => {
              if (c.link_id) return c.card_stock;
              const d = draftOf(c);
              const n = Number(d.qty);
              return d.product && Number.isInteger(n) && n >= 1
                ? Math.floor(Math.max(d.product.stock, 0) / n)
                : "-";
            },
          },
          {
            key: "price",
            label: t("colPrice", { platform }),
            hideMobile: true,
            render: (c) => (c.link_id && c.price ? price(c.price) : ""),
          },
          {
            key: "action",
            label: "",
            render: (c) =>
              c.link_id ? (
                <button
                  className="btn-ghost"
                  disabled={busy}
                  onClick={() => void act(() => unlink(c.link_id))}
                >
                  {t("unlink")}
                </button>
              ) : (
                <button
                  className="btn"
                  disabled={busy || !draftOf(c).product}
                  onClick={() => {
                    const d = draftOf(c);
                    if (d.product)
                      void act(() => link(c, d.product!.id, Number(d.qty)));
                  }}
                >
                  {t("link")}
                </button>
              ),
          },
        ]}
        rows={cards}
        rowId={cardKey}
        total={total}
        page={page}
        pageSize={pageSize}
        onPage={setPage}
        emptyTitle={t("empty")}
      />

      {line(msg)}

      {shared.map((g) => (
        <p key={g[0].product_id} className="hint">
          {t("shared", {
            title: g[0].product_title,
            stock: g[0].product_stock,
            n: g.length,
            platform,
            packs: g.map((c) => `${c.qty} → ${c.card_stock}`).join(", "),
          })}
        </p>
      ))}
    </div>
  );
}
