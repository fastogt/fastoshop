import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Product } from "./api";
import { toRubles } from "./money";
import DataTable, { type Selection, type Sort } from "./DataTable";
import Modal from "./Modal";
import PricingPanel from "./PricingPanel";
import ProductCard from "./ProductCard";
import {
  IconBox,
  IconEye,
  IconEyeOff,
  IconTag,
  IconTrash,
  IconDownload,
} from "./Icons";
import { useLang, useT } from "./i18n";
import { imageURL, isRemoteImage, thumbURL, useSign } from "./shop";
import { useJob } from "./useJob";
import JobBar from "./JobBar";

const kEmpty = {
  title: "",
  sku: "",
  description: "",
  price: 0,
  category: "",
  brand: "",
};

const kText = {
  hiddenBadge: { ru: "скрыт", en: "hidden" },
  bulkFill: { ru: "Заполнить", en: "Fill in" },
  fillPhotos: { ru: "Забрать фото к себе", en: "Bring the photos in" },
  fillPhotosHint: {
    ru: "Скачаем фотографии с сервера поставщика на наш и уменьшим их для каталога. Это займёт время и место на диске, зато магазин перестанет зависеть от чужого сервера.",
    en: "We download the photos from the supplier's server to ours and shrink them for the catalogue. It takes time and disk space, but the shop stops depending on somebody else's server.",
  },
  fillMain: { ru: "Только главные", en: "Main photos only" },
  fillAll: { ru: "Все фотографии", en: "Every photo" },
  fillMainHint: {
    ru: "Та, что видна в каталоге, в фидах и в поиске по картинкам",
    en: "The one shown in the catalogue, the feeds and image search",
  },
  fillAllHint: {
    ru: "Вместе с остальными, которые видно внутри карточки",
    en: "Together with the rest, seen inside the card",
  },
  fillCount: { ru: "{n} шт", en: "{n}" },
  fillDownload: { ru: "Скачать", en: "Download" },
  fillNothing: {
    ru: "Нечего скачивать: у выбранных товаров фото уже свои",
    en: "Nothing to download: the selected products already have their own photos",
  },
  fillStarted: { ru: "Качаем фото: {n}", en: "Downloading photos: {n}" },
  fillDone: {
    ru: "Готово. Скачано: {ok}, осталось ссылками: {failed}",
    en: "Done. Downloaded: {ok}, still links: {failed}",
  },
  allSuppliers: { ru: "Все поставщики", en: "All suppliers" },
  ownGoods: { ru: "Без поставщика", en: "No supplier" },
  thSupplier: { ru: "Поставщик", en: "Supplier" },
  thArticle: { ru: "Артикул", en: "Article" },
  rowHint: {
    ru: "Нажмите на строку, чтобы открыть товар. Отметьте строки - появятся действия над выбранными.",
    en: "Click a row to open the product. Tick rows and actions over the selection appear.",
  },
  bulkStock: { ru: "Проставить остаток", en: "Set stock" },
  bulkShow: { ru: "Показать на витрине", en: "Show on storefront" },
  bulkHide: { ru: "Скрыть с витрины", en: "Hide from storefront" },
  bulkGroup: { ru: "Перенести в группу", en: "Move to group" },
  bulkDelete: { ru: "Удалить", en: "Delete" },
  askStock: {
    ru: "Какой остаток проставить выбранным товарам?",
    en: "What stock should the selected products get?",
  },
  askGroup: {
    ru: "В какую группу перенести? Пусто - без поставщика.",
    en: "Which group to move them to? Empty means no supplier.",
  },
  askDelete: {
    ru: "Удалить выбранные товары ({n})? Вместе с ними исчезнут их адреса на витрине и связи с Ozon.",
    en: "Delete the selected products ({n})? Their storefront URLs and Ozon links go with them.",
  },
  bulkDone: { ru: "Изменено товаров: {n}", en: "Products changed: {n}" },
  bulkFailed: {
    ru: "Не получилось. Обновите страницу и попробуйте ещё раз.",
    en: "That did not work. Reload the page and try again.",
  },
  heading: { ru: "Товары", en: "Products" },
  searchPlaceholder: {
    ru: "Поиск по названию или артикулу",
    en: "Search by name or SKU",
  },
  add: { ru: "+ Добавить товар", en: "+ Add product" },
  noPhoto: {
    ru: "Фотографии нет. На витрине товар показывается заглушкой.",
    en: "No photo. The storefront shows a stub for this product.",
  },
  remoteHint: {
    ru: "Фото лежит на сервере поставщика, у нас его нет. Отметьте товары и нажмите «Заполнить», чтобы забрать фотографии к себе.",
    en: "The photo sits on the supplier’s server, we have no copy. Tick the products and press “Fill in” to bring the photos in.",
  },
  remotePhoto: {
    ru: "Фото лежит на чужом сервере. Закроют его - витрина останется без картинки. «Забрать фото к себе» скачает его к нам.",
    en: "This photo lives on someone else’s server. The day it goes, the storefront loses the picture. “Bring the photos in” downloads it to us.",
  },
  cancel: { ru: "Отмена", en: "Cancel" },
  nothingFound: {
    ru: "По запросу «{q}» ничего не нашлось.",
    en: "Nothing matched “{q}”.",
  },
  emptyFiltered: {
    ru: "По этому фильтру ничего нет. Товары есть в других группах - выберите «Все поставщики».",
    en: 'Nothing under this filter. There are products in other groups - pick "All suppliers".',
  },
  empty: {
    ru: "Товаров пока нет. Добавьте вручную или перенесите каталог с Ozon/WB на вкладке «Импорт».",
    en: "No products yet. Add one by hand, or bring your catalog over from Ozon/WB on the Import tab.",
  },
  thProduct: { ru: "Товар", en: "Product" },
  thPrice: { ru: "Цена", en: "Price" },
  thUpdated: { ru: "Изменён", en: "Changed" },
  thStock: { ru: "Остаток", en: "In stock" },
  outOfStock: { ru: "нет", en: "none" },
};

const isRemote = isRemoteImage;

export default function Products() {
  const [list, setList] = useState<Product[]>([]);
  const [edit, setEdit] = useState<Partial<Product> | null>(null);
  // Whether the shop has an AdHunters key: the button is not offered without
  // one, because there is nothing to pay the rewriting with.
  const [hasAIKey, setHasAIKey] = useState(false);
  const [page, setPage] = useState(1);
  const [per, setPer] = useState(100);
  const [sort, setSort] = useState<Sort>({ key: "created", desc: true });
  const [bulkMsg, setBulkMsg] = useState("");
  const [fill, setFill] = useState<{
    sel: Selection;
    main: number;
    total: number;
  } | null>(null);
  const [fillMainOnly, setFillMainOnly] = useState(true);
  const [total, setTotal] = useState(0);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [supplier, setSupplier] = useState<string | undefined>(undefined);
  const [suppliers, setSuppliers] = useState<string[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  // Suggestions from the page in hand, not an endpoint of their own: a brand is
  // typed once per product and the list only has to save the retyping.
  const brands = [...new Set(list.map((p) => p.brand).filter(Boolean))].sort();
  const t = useT(kText);
  const lang = useLang();
  const sign = useSign();

  const reload = useCallback(
    () =>
      api
        .products(
          page,
          query,
          supplier,
          per,
          sort.key,
          sort.desc ? "desc" : "asc",
        )
        .then((r) => {
          setList(r.products ?? []);
          setTotal(r.total || 0);
          // Deleting the last item on the last page: the backend answers with an
          // existing page, so follow it.
          if (r.page !== page) setPage(r.page || 1);
        }),
    [page, query, supplier, per, sort],
  );
  useEffect(() => {
    reload();
  }, [reload]);
  useEffect(() => {
    api.importSuppliers().then((r) => setSuppliers(r.suppliers));
    api.categories().then((r) => setCategories(r.categories));
    api.settings().then((s) => setHasAIKey(!!s.adhunters_api_key));
  }, []);

  // Typing in the search box must not hit the API on every keystroke.
  useEffect(() => {
    const timer = setTimeout(() => {
      setPage(1);
      setQuery(search.trim());
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);

  // One job per instance, so both the bar and the per-row spinners come from a
  // single state; when it finishes we re-read the page.
  const job = useJob(() => {
    void reload();
    if (jobResult.current) {
      setBulkMsg(
        t("fillDone", {
          ok: jobResult.current.ok,
          failed: jobResult.current.failed,
        }),
      );
    }
  });
  const jobResult = useRef<{ ok: number; failed: number } | null>(null);
  if (job && !job.running && job.result) {
    jobResult.current = { ok: job.result.imported, failed: job.result.errors };
  }
  const inFlight = new Set(job?.running ? (job.in_flight ?? []) : []);

  // While the job runs the page is re-read: rows already downloaded swap the
  // hotlink for their own thumbnail on their own.
  useEffect(() => {
    if (!job?.running) return;
    const id = setInterval(() => void reload(), 3000);
    return () => clearInterval(id);
  }, [job?.running, reload]);

  // The dialog asks before downloading because the two answers differ by a
  // third of the disk and a third of the wait, and the counts make that visible.
  const runFill = async (sel: Selection) => {
    const c = await api.bulkFillCount({
      ids: sel.ids,
      all: sel.all,
      q: query,
      supplier: supplier ?? null,
    });
    if (c.total === 0) {
      setBulkMsg(t("fillNothing"));
      return;
    }
    setFill({ sel, main: c.main, total: c.total });
  };

  const startFill = async (sel: Selection, mainOnly: boolean) => {
    setFill(null);
    setBulkMsg("");
    try {
      const r = await api.bulkFill({
        ids: sel.ids,
        all: sel.all,
        q: query,
        supplier: supplier ?? null,
        main_only: mainOnly,
      });
      setBulkMsg(
        r.started ? t("fillStarted", { n: r.total }) : t("fillNothing"),
      );
    } catch {
      setBulkMsg(t("bulkFailed"));
    }
  };

  const bulk = async (kind: string, sel: Selection) => {
    const scope = {
      ids: sel.ids,
      all: sel.all,
      q: query,
      supplier: supplier ?? null,
    };
    setBulkMsg("");
    try {
      let updated = 0;
      if (kind === "stock") {
        const v = prompt(t("askStock"), "1");
        if (v === null) return;
        const stock = Number(v);
        if (!Number.isFinite(stock) || stock < 0) return;
        updated = (await api.bulkStock({ ...scope, stock })).updated;
      } else if (kind === "show" || kind === "hide") {
        updated = (
          await api.bulkVisibility({ ...scope, hidden: kind === "hide" })
        ).updated;
      } else if (kind === "group") {
        const g = prompt(t("askGroup"), supplier ?? "");
        if (g === null) return;
        updated = (await api.bulkSupplier({ ...scope, new_supplier: g.trim() }))
          .updated;
      } else if (kind === "delete") {
        if (!confirm(t("askDelete", { n: sel.ids.length }))) return;
        updated = (await api.bulkDelete(sel.ids)).updated;
      }
      setBulkMsg(t("bulkDone", { n: updated }));
      await reload();
      const s = await api.importSuppliers();
      setSuppliers(s.suppliers);
    } catch {
      setBulkMsg(t("bulkFailed"));
    }
  };

  const afterSave = async () => {
    await reload();
    const [sup, cat] = await Promise.all([
      api.importSuppliers(),
      api.categories(),
    ]);
    setSuppliers(sup.suppliers);
    setCategories(cat.categories);
  };

  return (
    <div>
      <JobBar job={job} />

      <PricingPanel sample={list[0]} onRecomputed={reload} />

      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-bold">{t("heading")}</h1>
          <p className="hint mt-1">{t("rowHint")}</p>
          {list.some((p) => p.images?.[0] && isRemote(p.images[0].path)) && (
            <p className="hint mt-1">
              <span className="mr-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-amber-500 align-text-bottom text-[10px] leading-none font-bold text-white">
                !
              </span>
              {t("remoteHint")}
            </p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <input
            className="field w-64 max-w-full"
            placeholder={t("searchPlaceholder")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {suppliers.length > 0 && (
            <select
              className="field w-48 max-w-full"
              // "" is a real filter (goods with no supplier), so "all" needs a
              // value of its own rather than reusing the empty string.
              value={supplier === undefined ? "all" : supplier}
              onChange={(e) => {
                setPage(1);
                setSupplier(
                  e.target.value === "all" ? undefined : e.target.value,
                );
              }}
            >
              <option value="all">{t("allSuppliers")}</option>
              <option value="">{t("ownGoods")}</option>
              {suppliers.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          )}
          <button className="btn" onClick={() => setEdit({ ...kEmpty })}>
            {t("add")}
          </button>
        </div>
      </div>

      {edit && (
        <ProductCard
          initial={edit}
          suppliers={suppliers}
          categories={categories}
          brands={brands}
          hasAIKey={hasAIKey}
          onClose={() => setEdit(null)}
          onSaved={afterSave}
          onReload={reload}
        />
      )}

      {fill && (
        <Modal
          title={t("fillPhotos")}
          onClose={() => setFill(null)}
          footer={
            <>
              <button
                className="btn"
                onClick={() => void startFill(fill.sel, fillMainOnly)}
              >
                {t("fillDownload")}
              </button>
              <button className="btn-ghost" onClick={() => setFill(null)}>
                {t("cancel")}
              </button>
            </>
          }
        >
          <p className="hint">{t("fillPhotosHint")}</p>
          <div className="mt-4 flex flex-col gap-3">
            {[
              {
                only: true,
                label: t("fillMain"),
                hint: t("fillMainHint"),
                n: fill.main,
              },
              {
                only: false,
                label: t("fillAll"),
                hint: t("fillAllHint"),
                n: fill.total,
              },
            ].map((o) => (
              <label
                key={String(o.only)}
                className="border-line flex cursor-pointer items-start gap-3 rounded border p-3"
              >
                <input
                  type="radio"
                  className="mt-1"
                  checked={fillMainOnly === o.only}
                  onChange={() => setFillMainOnly(o.only)}
                />
                <span>
                  <span className="font-semibold">{o.label}</span>
                  {" - "}
                  {t("fillCount", { n: o.n })}
                  <span className="hint block">{o.hint}</span>
                </span>
              </label>
            ))}
          </div>
        </Modal>
      )}

      <DataTable<Product>
        columns={[
          {
            key: "title",
            label: t("thProduct"),
            sortable: true,
            render: (p) => (
              <div className="flex items-center gap-3">
                <span className="relative inline-block h-10 w-10 shrink-0">
                  {p.images?.[0] ? (
                    <img
                      src={thumbURL(p.images[0].path)}
                      alt=""
                      loading="lazy"
                      // Two steps down, not one. A missing small copy means the
                      // photo predates thumbnails, and the original is still
                      // there - falling straight to the stub would claim the
                      // product has no picture when it has one. Only when the
                      // original fails too does the row show the shop's own
                      // mark: a supplier's link can stop answering and start
                      // again a day later, so nothing is deleted over it.
                      onError={(e) => {
                        const el = e.currentTarget;
                        const full = new URL(
                          imageURL(p.images![0].path),
                          location.href,
                        ).href;
                        if (el.src !== full) {
                          el.src = full;
                          return;
                        }
                        el.src = "/nophoto.svg";
                        el.classList.add("opacity-60");
                      }}
                      className="h-10 w-10 rounded object-cover"
                    />
                  ) : (
                    // The storefront's own stub, so the admin and the shop agree
                    // on what a product without a picture looks like.
                    <img
                      src="/nophoto.svg"
                      alt=""
                      title={t("noPhoto")}
                      className="border-line h-10 w-10 rounded border object-contain opacity-60"
                    />
                  )}
                  {p.images?.[0] && isRemote(p.images[0].path) && (
                    <span
                      title={t("remotePhoto")}
                      className="absolute -top-1 -left-1 flex h-4 w-4 items-center justify-center rounded-full bg-amber-500 text-[10px] leading-none font-bold text-white"
                    >
                      !
                    </span>
                  )}
                  {inFlight.has(p.id) && (
                    <span className="absolute inset-0 flex items-center justify-center rounded bg-white/70">
                      <span className="border-brand h-4 w-4 animate-spin rounded-full border-2 border-t-transparent" />
                    </span>
                  )}
                </span>
                <span className="font-medium">{p.title}</span>
                {p.hidden && (
                  <span className="text-muted border-line rounded border px-2 py-0.5 text-xs">
                    {t("hiddenBadge")}
                  </span>
                )}
              </div>
            ),
          },
          {
            key: "sku",
            label: t("thArticle"),
            sortable: true,
            hideMobile: true,
            render: (p) => p.sku || "-",
          },
          {
            key: "price",
            label: t("thPrice"),
            sortable: true,
            width: "110px",
            render: (p) => (
              <span className="whitespace-nowrap">
                {toRubles(p.price)} {sign}
              </span>
            ),
          },
          {
            key: "stock",
            label: t("thStock"),
            sortable: true,
            render: (p) =>
              p.stock > 0 ? (
                p.stock
              ) : (
                <span className="text-muted">{t("outOfStock")}</span>
              ),
          },
          {
            key: "updated",
            label: t("thUpdated"),
            sortable: true,
            hideMobile: true,
            width: "110px",
            render: (p) => (
              <span className="text-muted whitespace-nowrap">
                {new Date(p.updated_at).toLocaleDateString(lang, {
                  day: "2-digit",
                  month: "2-digit",
                  year: "2-digit",
                })}
              </span>
            ),
          },
          {
            key: "supplier",
            label: t("thSupplier"),
            hideMobile: true,
            render: (p) => (
              <span className="text-muted">{p.supplier || "-"}</span>
            ),
          },
        ]}
        rows={list}
        rowId={(p) => p.id}
        total={total}
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
        bulkActions={[
          {
            label: t("bulkStock"),
            icon: <IconBox />,
            onClick: (sel) => bulk("stock", sel),
          },
          {
            label: t("bulkShow"),
            icon: <IconEye />,
            onClick: (sel) => bulk("show", sel),
          },
          {
            label: t("bulkHide"),
            icon: <IconEyeOff />,
            onClick: (sel) => bulk("hide", sel),
          },
          {
            label: t("bulkGroup"),
            icon: <IconTag />,
            onClick: (sel) => bulk("group", sel),
          },
          {
            label: t("bulkFill"),
            icon: <IconDownload />,
            onClick: (sel) => void runFill(sel),
          },
          {
            label: t("bulkDelete"),
            icon: <IconTrash />,
            danger: true,
            // There is no "delete everything by filter" mode: a single button
            // that wipes the catalogue along with its indexed URLs is not worth
            // it.
            idsOnly: true,
            onClick: (sel) => bulk("delete", sel),
          },
        ]}
        onRowClick={setEdit}
        emptyTitle={
          query
            ? t("nothingFound", { q: query })
            : supplier !== undefined
              ? t("emptyFiltered")
              : t("empty")
        }
      />

      {bulkMsg && <p className="text-green-700">{bulkMsg}</p>}
    </div>
  );
}
