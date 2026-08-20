import { useCallback, useEffect, useRef, useState } from "react";
import { api, apiError, type PriceRule, type Product } from "./api";
import { toRubles, toMinor } from "./money";
import DataTable, { type Selection, type Sort } from "./DataTable";
import Modal from "./Modal";
import {
  IconBox,
  IconEye,
  IconEyeOff,
  IconTag,
  IconTrash,
  IconDownload,
} from "./Icons";
import { useT } from "./i18n";
import { imageURL, isRemoteImage, useSign } from "./shop";
import { useJob } from "./useJob";
import JobBar from "./JobBar";

const kEmpty = {
  title: "",
  sku: "",
  description: "",
  price: 0,
  category: "",
};

const kText = {
  showOnStorefront: {
    ru: "Показывать на витрине",
    en: "Show on the storefront",
  },
  hiddenHint: {
    ru: "Скрытый товар пропадает из каталога, из карты сайта и не открывается по прямой ссылке. На публикацию в каналах это не влияет.",
    en: "A hidden product disappears from the catalogue and the sitemap, and its page stops opening. Channel publication is not affected.",
  },
  hiddenBadge: { ru: "скрыт", en: "hidden" },
  bulkFill: { ru: "Заполнить", en: "Fill in" },
  fillPhotos: { ru: "Забрать фото к себе", en: "Bring the photos in" },
  fillPhotosHint: {
    ru: "Скачаем фотографии с сервера поставщика на наш и уменьшим их для каталога. Это займёт время и место на диске, зато магазин перестанет зависеть от чужого сервера.",
    en: "We download the photos from the supplier's server to ours and shrink them for the catalogue. It takes time and disk space, but the shop stops depending on somebody else's server.",
  },
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
    ru: "Нажмите на строку, чтобы открыть товар. Отметьте строки — появятся действия над выбранными.",
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
    ru: "В какую группу перенести? Пусто — без поставщика.",
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
  labelTitle: { ru: "Название *", en: "Name *" },
  titlePlaceholder: {
    ru: "Чайник эмалированный 2 л",
    en: "Enamel kettle, 2 L",
  },
  labelSku: { ru: "Артикул (SKU)", en: "SKU" },
  labelPrice: { ru: "Цена, {sign}", en: "Price, {sign}" },
  labelStock: { ru: "Остаток", en: "In stock" },
  labelWeight: { ru: "Вес, г", en: "Weight, g" },
  labelSize: { ru: "Габариты, мм", en: "Size, mm" },
  sizeHint: {
    ru: "Длина × ширина × высота упаковки. По весу и габаритам считается доставка, поэтому пустое поле честнее нуля: незаполненный вес — это «неизвестно», а не «невесомый».",
    en: "Length × width × height of the parcel. Delivery is priced by weight and size, so an empty field is more honest than a zero: an unstated weight means \u201cunknown\u201d, not \u201cweightless\u201d.",
  },
  labelCategory: { ru: "Категория", en: "Category" },
  categoryHint: {
    ru: "Косая черта задаёт вложенность: «Посуда/Кастрюли» — это страница «Кастрюли» внутри «Посуды». У каждого уровня своя страница на витрине.",
    en: 'A slash makes a level: "Cookware/Pots" is a Pots page inside Cookware. Every level gets a page of its own on the storefront.',
  },
  skuLocked: {
    ru: "Артикул связывает товар с выгрузкой поставщика — по нему загрузка находит, что обновлять. Чтобы изменить, сначала уберите поставщика.",
    en: "The article is what links this product to its supplier's feed — an import finds what to update by it. To change it, clear the supplier first.",
  },
  enrich: { ru: "Улучшить текст (AI)", en: "Improve the text (AI)" },
  enriching: { ru: "Пишем…", en: "Writing…" },
  enrichHint: {
    ru: "Название и описание перепишет модель — проверьте факты перед сохранением. За карточку отвечаете вы. Пока не нажали «Сохранить», в магазине ничего не изменилось.",
    en: "A model rewrites the name and the description — check the facts before saving. The card is your responsibility. Until you press Save, nothing in the shop has changed.",
  },
  labelDescription: { ru: "Описание", en: "Description" },
  descriptionPlaceholder: {
    ru: "Что это, из чего сделано, кому подойдёт — этот текст читают и покупатели, и поисковики.",
    en: "What it is, what it is made of, who it suits — this text is read by shoppers and search engines alike.",
  },
  labelPhotos: { ru: "Фотографии", en: "Photos" },
  addPhoto: { ru: "Добавить", en: "Add" },
  removePhoto: { ru: "Удалить фото", en: "Remove photo" },
  pricing: { ru: "Цены", en: "Prices" },
  pricingHint: {
    ru: "Как из цены поставщика получается цена на витрине. Цены, которые вы правили руками, пересчёт не трогает.",
    en: "How a supplier's price becomes the one on the shelf. Prices you edited by hand are left alone.",
  },
  rate: { ru: "Курс закупки", en: "Cost rate" },
  rateHint: {
    ru: "Во сколько раз цена поставщика превращается в вашу валюту. Прайс в той же валюте — оставьте 1.",
    en: "What the supplier's price is multiplied by to become your money. A price list in your own currency needs 1.",
  },
  markup: { ru: "Наценка, %", en: "Markup, %" },
  markupHint: {
    ru: "Сколько добавляем сверх закупки. 30 — это ×1.3.",
    en: "How much is added on top of the cost. 30 means ×1.3.",
  },
  bandsToggle: {
    ru: "Разная наценка для дешёвых и дорогих товаров",
    en: "A different markup for cheap and dear goods",
  },
  bandsHint: {
    ru: "На товаре за 7 рублей одна наценка ничего не оставляет, а на товаре за 300 выносит цену выше магазина бренда. Полоса — до какой закупочной цены какой множитель; последняя строка «и выше» обязательна.",
    en: 'One markup leaves nothing on a 7-rouble item and prices a 300-rouble one above the brand\'s own store. A band is: up to which cost, which multiplier; the final "and above" row is required.',
  },
  bandUpTo: { ru: "до", en: "up to" },
  bandAbove: { ru: "и выше", en: "and above" },
  bandMultiplier: { ru: "множитель", en: "multiplier" },
  addBand: { ru: "Добавить полосу", en: "Add a band" },
  removeBand: { ru: "Удалить", en: "Remove" },
  savePricing: { ru: "Сохранить", en: "Save" },
  pricingSaved: {
    ru: "Сохранено. Цены пока прежние — нажмите «Пересчитать цены».",
    en: 'Saved. No price has moved yet — press "Recompute prices".',
  },
  recompute: { ru: "Пересчитать цены", en: "Recompute prices" },
  recomputed: {
    ru: "Пересчитано товаров: {n}",
    en: "Products recomputed: {n}",
  },
  chainExample: {
    ru: "Закупка {source} → курс {rate} → {cost} → наценка → {shelf}",
    en: "Cost {source} → rate {rate} → {cost} → markup → {shelf}",
  },
  dragHint: {
    ru: "Фотографии можно перетаскивать: первая уходит в поиск, в каталог и в карточку канала.",
    en: "Photos can be dragged: the first one goes to search, to the catalogue and to a channel listing.",
  },
  photosHint: {
    ru: "JPEG, PNG или WebP, до 10 МБ. Первое фото попадает в поисковую выдачу и в карточку канала.",
    en: "JPEG, PNG or WebP, up to 10 MB. The first photo is what search results and the channel card show.",
  },
  noPhoto: {
    ru: "Фотографии нет. На витрине товар показывается заглушкой.",
    en: "No photo. The storefront shows a stub for this product.",
  },
  remoteHint: {
    ru: "Фото лежит на сервере поставщика, у нас его нет. Отметьте товары и нажмите «Заполнить», чтобы забрать фотографии к себе.",
    en: "The photo sits on the supplier’s server, we have no copy. Tick the products and press “Fill in” to bring the photos in.",
  },
  remotePhoto: {
    ru: "Фото лежит на чужом сервере. Закроют его — витрина останется без картинки. «Забрать фото к себе» скачает его к нам.",
    en: "This photo lives on someone else’s server. The day it goes, the storefront loses the picture. “Bring the photos in” downloads it to us.",
  },
  labelSupplier: { ru: "Поставщик (группа)", en: "Supplier (group)" },
  supplierPlaceholder: { ru: "без поставщика", en: "no supplier" },
  fieldsHint: {
    ru: "Категория — раздел витрины, по ней покупатель фильтрует каталог; импорт её не заполняет. Поставщик — чей это товар: только его выгрузка будет обновлять цену и остаток. Оставьте пустым, и товар не тронет ни одна загрузка. Группу можно менять и вписывать новую — она появится сама, заводить её заранее не нужно. Осторожно с переносом: если в выгрузке этой группы такого артикула нет, ближайшая загрузка обнулит остаток товара.",
    en: "Category is a storefront section customers filter by; import never fills it. Supplier is whose goods these are: only that feed updates the price and stock. Leave it empty and no import will touch the product. The group can be changed and a new name typed in — it appears by itself, there is nothing to create up front. Mind the move: if that group’s feed has no such article, the next import zeroes the product’s stock.",
  },
  editTitle: { ru: "Товар", en: "Product" },
  newTitle: { ru: "Новый товар", en: "New product" },
  photosAfterSave: {
    ru: "Фотографии можно добавить после сохранения.",
    en: "You can add photos once the product is saved.",
  },
  save: { ru: "Сохранить", en: "Save" },
  cancel: { ru: "Отмена", en: "Cancel" },
  nothingFound: {
    ru: "По запросу «{q}» ничего не нашлось.",
    en: "Nothing matched “{q}”.",
  },
  emptyFiltered: {
    ru: "По этому фильтру ничего нет. Товары есть в других группах — выберите «Все поставщики».",
    en: 'Nothing under this filter. There are products in other groups — pick "All suppliers".',
  },
  empty: {
    ru: "Товаров пока нет. Добавьте вручную или перенесите каталог с Ozon/WB на вкладке «Импорт».",
    en: "No products yet. Add one by hand, or bring your catalog over from Ozon/WB on the Import tab.",
  },
  thProduct: { ru: "Товар", en: "Product" },
  thPrice: { ru: "Цена", en: "Price" },
  thStock: { ru: "Остаток", en: "In stock" },
  outOfStock: { ru: "нет", en: "none" },
};

const isRemote = isRemoteImage;

// An empty field is "nobody said", which the server stores as NULL — not as a
// zero a delivery quote would take for a real measurement.
const numOrNull = (v: string): number | null => {
  const n = Number(v.trim());
  return v.trim() === "" || !Number.isFinite(n) || n <= 0 ? null : n;
};

export default function Products() {
  const [list, setList] = useState<Product[]>([]);
  const [edit, setEdit] = useState<Partial<Product> | null>(null);
  // Index of the photo being dragged. A ref, not state: it changes on every
  // dragover and re-rendering the strip mid-drag drops the drag itself.
  const dragFrom = useRef<number | null>(null);
  // Pricing: the rate that carries a supplier's price into our money, and the
  // markup on top. Both live here, next to the prices they produce — they used
  // to sit on the Import tab, a tab away from anything they change.
  const [rate, setRate] = useState("1");
  const [rules, setRules] = useState<PriceRule[]>([]);
  const [priceMsg, setPriceMsg] = useState("");
  // Whether the shop has an AdHunters key: the button is not offered without
  // one, because there is nothing to pay the rewriting with.
  const [hasAIKey, setHasAIKey] = useState(false);
  const [enriching, setEnriching] = useState(false);
  const [enrichMsg, setEnrichMsg] = useState("");
  const [priceRub, setPriceRub] = useState("0");
  // null = the stock field was never touched. Sending it means re-declaring the
  // physical stock: a form opened before a sale would resurrect sold units.
  const [stock, setStock] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const [per, setPer] = useState(100);
  const [sort, setSort] = useState<Sort>({ key: "created", desc: true });
  const [bulkMsg, setBulkMsg] = useState("");
  const [total, setTotal] = useState(0);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [supplier, setSupplier] = useState<string | undefined>(undefined);
  const [suppliers, setSuppliers] = useState<string[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const t = useT(kText);
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

  const open = (p: Partial<Product>) => {
    setEdit(p);
    setPriceRub(toRubles(p.price ?? 0));
    setStock(null);
  };

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

  const runFill = async (sel: Selection) => {
    if (!window.confirm(`${t("fillPhotos")}\n\n${t("fillPhotosHint")}`)) return;
    setBulkMsg("");
    try {
      const r = await api.bulkFill({
        ids: sel.ids,
        all: sel.all,
        q: query,
        supplier: supplier ?? null,
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

  // The draft lands straight in the form: the dialog is already a draft —
  // nothing reaches the database until Save, and closing the window undoes it.
  const enrich = async () => {
    if (!edit?.id) return;
    setEnrichMsg("");
    setEnriching(true);
    try {
      const d = await api.enrichProduct(edit.id);
      setEdit((prev) =>
        prev ? { ...prev, title: d.title, description: d.description,
          category: d.category || prev.category } : prev,
      );
    } catch (e) {
      setEnrichMsg(apiError(e) ?? t("bulkFailed"));
    } finally {
      setEnriching(false);
    }
  };

  const save = async () => {
    if (!edit?.title) return;
    const p = { ...edit, price: toMinor(priceRub) };
    delete p.stock;
    if (stock !== null) p.stock = stock;
    // supplier is always sent explicitly: the field is in the form, and an
    // empty value is a deliberate "no supplier" choice, not "leave as is".
    p.supplier = edit.supplier ?? "";
    if (edit.id) await api.updateProduct(edit.id, p);
    else await api.createProduct(p);
    setEdit(null);
    await reload();
    const [sup, cat] = await Promise.all([
      api.importSuppliers(),
      api.categories(),
    ]);
    setSuppliers(sup.suppliers);
    setCategories(cat.categories);
  };

  useEffect(() => {
    api.priceRules().then((r) => {
      setRate(String(r.coefficient || 1));
      setRules(r.rules);
    });
  }, []);

  // The percentage and the ladder are one thing stored one way: a single band
  // "and above" is a plain markup, several bands are a ladder — and no rules
  // at all is a 0% markup that was never set, not a hidden field.
  const markup =
    rules.length <= 1
      ? Math.round(((rules[0]?.multiplier ?? 1) - 1) * 100)
      : null;
  const savePricing = async (next: PriceRule[]) => {
    setPriceMsg("");
    try {
      const r = await api.setPriceRules(next);
      setRules(r.rules);
      setPriceMsg(t("pricingSaved"));
    } catch (e) {
      setPriceMsg(apiError(e) ?? t("bulkFailed"));
    }
  };
  const recompute = async () => {
    setPriceMsg("");
    try {
      // The server recomputes from the *stored* ladder, so what is on the
      // screen is saved first — otherwise the button reprices the catalogue
      // by numbers the owner just replaced and reports success.
      await api.setPriceRules(rules);
      const r = await api.recomputePrices(Number(rate.replace(",", ".")) || 1);
      setPriceMsg(t("recomputed", { n: r.updated }));
      await reload();
    } catch (e) {
      setPriceMsg(apiError(e) ?? t("bulkFailed"));
    }
  };

  return (
    <div>
      <JobBar job={job} />

      {/* Folded away: pricing is set once and then left alone, while the table
          below is the daily work. Open, it answers the one question the numbers
          in the price column raise — where they came from. */}
      <details className="card mb-5">
        <summary className="cursor-pointer font-bold">{t("pricing")}</summary>
        <p className="hint mt-2">{t("pricingHint")}</p>

        <div className="mt-3 flex flex-wrap items-start gap-6">
          <div>
            <label className="label">{t("rate")}</label>
            <input
              className="field w-32"
              inputMode="decimal"
              value={rate}
              onChange={(e) => setRate(e.target.value)}
            />
            <p className="hint mt-1 max-w-xs">{t("rateHint")}</p>
          </div>

          {markup !== null && (
            <div>
              <label className="label">{t("markup")}</label>
              <input
                className="field w-24"
                inputMode="decimal"
                value={String(markup)}
                onChange={(e) =>
                  setRules([
                    {
                      up_to: 0,
                      multiplier:
                        1 +
                        (Number(e.target.value.replace(",", ".")) || 0) / 100,
                    },
                  ])
                }
              />
              <p className="hint mt-1 max-w-xs">{t("markupHint")}</p>
            </div>
          )}
        </div>

        {/* The chain on a real row: three arrows say more than a paragraph, and
            they show that the number in the price column is not an accident. */}
        {list[0]?.source_price ? (
          <p className="hint mt-3">
            {t("chainExample", {
              source: String(toRubles(list[0].source_price)),
              rate,
              cost: `${toRubles(Math.round(list[0].source_price * (Number(rate.replace(",", ".")) || 1)))} ${sign}`,
              shelf: `${toRubles(list[0].price ?? 0)} ${sign}`,
            })}
          </p>
        ) : null}

        {/* The editor follows the data, not a toggle of its own: bands on
            screen mean bands in the ladder, and deleting down to one row
            brings the plain percent field back. A hidden multi-band ladder
            would still be saved and still price the catalogue. */}
        {rules.length <= 1 && (
          <div className="mt-4">
            <button
              className="text-brand cursor-pointer text-sm underline"
              onClick={() =>
                setRules([
                  { up_to: 5000, multiplier: 1.5 },
                  ...(rules.length ? rules : [{ up_to: 0, multiplier: 1.3 }]),
                ])
              }
            >
              {t("bandsToggle")}
            </button>
          </div>
        )}

        {rules.length > 1 && (
          <div className="border-line mt-3 flex flex-col gap-2 border-t pt-3">
            <p className="hint max-w-2xl">{t("bandsHint")}</p>
            {rules.map((rule, i) => (
              <div key={i} className="flex items-center gap-2">
                <span className="hint w-10">{t("bandUpTo")}</span>
                {rule.up_to === 0 ? (
                  <span className="w-28 font-semibold">{t("bandAbove")}</span>
                ) : (
                  <input
                    className="field w-28"
                    inputMode="decimal"
                    value={toRubles(rule.up_to)}
                    onChange={(e) =>
                      setRules(
                        rules.map((x, j) =>
                          j === i
                            ? { ...x, up_to: toMinor(e.target.value) }
                            : x,
                        ),
                      )
                    }
                  />
                )}
                <span className="hint">{t("bandMultiplier")}</span>
                <input
                  className="field w-20"
                  inputMode="decimal"
                  value={String(rule.multiplier)}
                  onChange={(e) =>
                    setRules(
                      rules.map((x, j) =>
                        j === i
                          ? {
                              ...x,
                              multiplier:
                                Number(e.target.value.replace(",", ".")) || 0,
                            }
                          : x,
                      ),
                    )
                  }
                />
                <button
                  className="text-muted cursor-pointer text-sm hover:text-red-600"
                  onClick={() => setRules(rules.filter((_, j) => j !== i))}
                >
                  {t("removeBand")}
                </button>
              </div>
            ))}
            <div>
              <button
                className="btn-ghost"
                onClick={() =>
                  setRules([
                    ...rules.filter((r) => r.up_to !== 0),
                    { up_to: 5000, multiplier: 1.5 },
                    ...rules.filter((r) => r.up_to === 0),
                  ])
                }
              >
                {t("addBand")}
              </button>
            </div>
          </div>
        )}

        <div className="mt-4 flex flex-wrap items-center gap-3">
          <button className="btn-ghost" onClick={() => void savePricing(rules)}>
            {t("savePricing")}
          </button>
          <button className="btn" onClick={() => void recompute()}>
            {t("recompute")}
          </button>
          {priceMsg && <span className="text-green-700">{priceMsg}</span>}
        </div>
      </details>

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
        <div className="flex items-center gap-3">
          <input
            className="field w-64"
            placeholder={t("searchPlaceholder")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {suppliers.length > 0 && (
            <select
              className="field w-48"
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
          <button className="btn" onClick={() => open({ ...kEmpty })}>
            {t("add")}
          </button>
        </div>
      </div>

      {edit && (
        <Modal
          title={edit.id ? t("editTitle") : t("newTitle")}
          onClose={() => setEdit(null)}
          footer={
            <>
              <button className="btn" onClick={save}>
                {t("save")}
              </button>
              <button className="btn-ghost" onClick={() => setEdit(null)}>
                {t("cancel")}
              </button>
            </>
          }
        >
          <div className="flex flex-col gap-4">
            <div>
              <label className="label">{t("labelTitle")}</label>
              <input
                className="field"
                placeholder={t("titlePlaceholder")}
                value={edit.title ?? ""}
                onChange={(e) => setEdit({ ...edit, title: e.target.value })}
              />
            </div>
            <div className="flex flex-wrap gap-3">
              <div className="min-w-40 flex-1">
                <label className="label">{t("labelSku")}</label>
                {/* The article is what an import matches a product by. Change
                    it on a supplier's goods and the next feed finds no match:
                    it creates a duplicate and zeroes this product's stock, a
                    week later and silently. Own goods have no feed to break. */}
                <input
                  className="field"
                  placeholder="CH-201"
                  readOnly={!!edit.supplier}
                  value={edit.sku ?? ""}
                  onChange={(e) => setEdit({ ...edit, sku: e.target.value })}
                />
                {edit.supplier && <p className="hint mt-1">{t("skuLocked")}</p>}
              </div>
              <div className="w-36">
                <label className="label">{t("labelPrice", { sign })}</label>
                <input
                  className="field"
                  inputMode="decimal"
                  value={priceRub}
                  onChange={(e) => setPriceRub(e.target.value)}
                />
              </div>
              <div className="w-28">
                <label className="label">{t("labelStock")}</label>
                <input
                  className="field"
                  type="number"
                  value={stock ?? edit.stock ?? 0}
                  onChange={(e) => setStock(Number(e.target.value))}
                />
              </div>
              <div className="w-28">
                <label className="label">{t("labelWeight")}</label>
                <input
                  className="field"
                  type="number"
                  min="0"
                  value={edit.weight_g ?? ""}
                  onChange={(e) =>
                    setEdit({ ...edit, weight_g: numOrNull(e.target.value) })
                  }
                />
              </div>
              <div className="min-w-40 flex-1">
                <label className="label">{t("labelSupplier")}</label>
                <input
                  className="field"
                  list="product-suppliers"
                  placeholder={t("supplierPlaceholder")}
                  value={edit.supplier ?? ""}
                  onChange={(e) =>
                    setEdit({ ...edit, supplier: e.target.value })
                  }
                />
                <datalist id="product-suppliers">
                  {suppliers.map((x) => (
                    <option key={x} value={x} />
                  ))}
                </datalist>
              </div>
            </div>
            <div>
              <label className="label">{t("labelSize")}</label>
              <div className="flex items-center gap-2">
                {(["length_mm", "width_mm", "height_mm"] as const).map((k, i) => (
                  <div key={k} className="flex items-center gap-2">
                    {i > 0 && <span className="text-muted">×</span>}
                    <input
                      className="field w-24"
                      type="number"
                      min="0"
                      value={edit[k] ?? ""}
                      onChange={(e) =>
                        setEdit({ ...edit, [k]: numOrNull(e.target.value) })
                      }
                    />
                  </div>
                ))}
              </div>
              <p className="hint mt-1">{t("sizeHint")}</p>
            </div>
            {/* A category is a path, and paths are long: its own full-width row,
                not a quarter of a row next to the price. */}
            <div>
              <label className="label">{t("labelCategory")}</label>
              <input
                className="field"
                list="product-categories"
                placeholder="Посуда/Кастрюли"
                value={edit.category ?? ""}
                onChange={(e) => setEdit({ ...edit, category: e.target.value })}
              />
              <datalist id="product-categories">
                {categories.map((c) => (
                  <option key={c} value={c} />
                ))}
              </datalist>
              <p className="hint mt-1">{t("categoryHint")}</p>
            </div>
            <p className="hint -mt-2">{t("fieldsHint")}</p>
            <div>
              <label className="label">{t("labelDescription")}</label>
              <textarea
                className="field"
                rows={4}
                placeholder={t("descriptionPlaceholder")}
                value={edit.description ?? ""}
                onChange={(e) =>
                  setEdit({ ...edit, description: e.target.value })
                }
              />
              {hasAIKey && edit.id && (
                <div className="mt-2">
                  <button
                    className="btn-ai"
                    disabled={enriching}
                    onClick={() => void enrich()}
                  >
                    {enriching && (
                      <span className="border-brand h-4 w-4 animate-spin rounded-full border-2 border-t-transparent" />
                    )}
                    {enriching ? t("enriching") : `✨ ${t("enrich")}`}
                  </button>
                  <p className="hint mt-1">{t("enrichHint")}</p>
                  {enrichMsg && (
                    <p className="mt-1 text-sm text-red-600">{enrichMsg}</p>
                  )}
                </div>
              )}
            </div>
            {edit.id ? (
              <div>
                <label className="label">{t("labelPhotos")}</label>
                {(edit.images?.length ?? 0) > 1 && (
                  <p className="hint mb-2">{t("dragHint")}</p>
                )}
                <div className="flex flex-wrap items-center gap-3">
                  {edit.images?.map((im, i) => (
                    <span
                      key={im.id}
                      className="group relative cursor-grab active:cursor-grabbing"
                      draggable
                      onDragStart={() => (dragFrom.current = i)}
                      onDragOver={(e) => e.preventDefault()}
                      onDrop={() => {
                        const from = dragFrom.current;
                        dragFrom.current = null;
                        if (from === null || from === i || !edit.images) return;
                        const next = [...edit.images];
                        next.splice(i, 0, ...next.splice(from, 1));
                        setEdit({ ...edit, images: next });
                        // Saved at once: the order is a property of the product,
                        // not of the form, and losing it to a cancelled dialog
                        // would be its own surprise.
                        void api
                          .setImageOrder(
                            edit.id!,
                            next.map((x) => x.id),
                          )
                          .then(() => reload())
                          .catch(() => setEdit({ ...edit }));
                      }}
                    >
                      <img
                        src={imageURL(im.path)}
                        alt=""
                        // Same rule as the list: a supplier's link that stops
                        // answering shows the shop's mark, and starts working
                        // again by itself the day the link does.
                        onError={(e) => {
                          e.currentTarget.src = "/nophoto.svg";
                          e.currentTarget.classList.add("opacity-60");
                        }}
                        className="border-line h-20 w-20 rounded-lg border object-cover"
                      />
                      {isRemote(im.path) && (
                        <span
                          title={t("remotePhoto")}
                          className="absolute -top-1 -left-1 flex h-5 w-5 items-center justify-center rounded-full bg-amber-500 text-xs font-bold text-white"
                        >
                          !
                        </span>
                      )}
                      <button
                        title={t("removePhoto")}
                        className="border-line absolute -top-2 -right-2 hidden h-6 w-6 cursor-pointer rounded-full border bg-white text-sm text-red-600 group-hover:block"
                        onClick={async () => {
                          if (edit.id)
                            setEdit(await api.deleteImage(edit.id, im.id));
                        }}
                      >
                        ×
                      </button>
                    </span>
                  ))}
                  {/* The native file input shows "No file chosen" and resists
                    styling — we hide it behind a button-styled label. */}
                  <label className="btn-ghost border-line flex h-20 w-20 cursor-pointer flex-col items-center justify-center gap-1 border-2 border-dashed text-center text-xs">
                    <span className="text-lg leading-none">+</span>
                    {t("addPhoto")}
                    <input
                      type="file"
                      accept="image/jpeg,image/png,image/webp"
                      className="hidden"
                      onChange={async (e) => {
                        const f = e.target.files?.[0];
                        if (f && edit.id)
                          setEdit(await api.uploadImage(edit.id, f));
                        e.target.value = "";
                      }}
                    />
                  </label>
                </div>
                <p className="hint mt-1">{t("photosHint")}</p>
              </div>
            ) : (
              <p className="hint">{t("photosAfterSave")}</p>
            )}
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={!edit.hidden}
                onChange={(e) =>
                  setEdit({ ...edit, hidden: !e.target.checked })
                }
              />
              <span>{t("showOnStorefront")}</span>
            </label>
            <p className="hint -mt-2">{t("hiddenHint")}</p>
            {/* ponytail: per-channel toggles (Kufar/Avito) land in phase 2 together with the adapters */}
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
                      src={imageURL(p.images[0].path)}
                      alt=""
                      // A supplier's link can stop answering and start again a
                      // day later, so nothing is deleted over it: the row shows
                      // the shop's own mark until the picture is back.
                      onError={(e) => {
                        e.currentTarget.src = "/nophoto.svg";
                        e.currentTarget.classList.add("opacity-60");
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
            render: (p) => p.sku || "—",
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
            key: "supplier",
            label: t("thSupplier"),
            hideMobile: true,
            render: (p) => (
              <span className="text-muted">{p.supplier || "—"}</span>
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
        onRowClick={open}
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
