import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Product } from "./api";
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
import { useSign } from "./shop";
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
  fillTitle: { ru: "Что заполнить", en: "What to fill in" },
  fillPhotos: { ru: "Забрать фото к себе", en: "Bring the photos in" },
  fillPhotosHint: {
    ru: "Фото импортированных товаров лежат ссылками на сервер поставщика: закроет доступ — витрина останется без картинок. Скачаем их к себе и сделаем уменьшенные копии для плитки каталога — без них страница тянет полноразмерные снимки. Каталог на 24 тысячи товаров — это несколько гигабайт на диске и десятки минут работы; что не скачается, останется ссылкой.",
    en: "Photos of imported products are links to the supplier's server: the day they close it, the storefront loses its pictures. This brings them onto our own disk and makes the small copies the catalogue grid needs — without them a page pulls full-size photos. A catalogue of 24 thousand products means several gigabytes and tens of minutes; anything that fails stays a link.",
  },
  fillRun: { ru: "Запустить", en: "Start" },
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
  labelCategory: { ru: "Категория", en: "Category" },
  labelDescription: { ru: "Описание", en: "Description" },
  descriptionPlaceholder: {
    ru: "Что это, из чего сделано, кому подойдёт — этот текст читают и покупатели, и поисковики.",
    en: "What it is, what it is made of, who it suits — this text is read by shoppers and search engines alike.",
  },
  labelPhotos: { ru: "Фотографии", en: "Photos" },
  addPhoto: { ru: "Добавить", en: "Add" },
  removePhoto: { ru: "Удалить фото", en: "Remove photo" },
  photosHint: {
    ru: "JPEG, PNG или WebP, до 10 МБ. Первое фото попадает в поисковую выдачу и в карточку канала.",
    en: "JPEG, PNG or WebP, up to 10 MB. The first photo is what search results and the channel card show.",
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
  prev: { ru: "← Назад", en: "← Back" },
  next: { ru: "Дальше →", en: "Next →" },
  pageOf: {
    ru: "{page} из {pages} · всего: {total}",
    en: "{page} of {pages} · total: {total}",
  },
};

// Prices are stored in minor units, but the shop owner thinks in rubles.
const toRubles = (minor: number) => (minor / 100).toFixed(2);
const toMinor = (rubles: string) =>
  Math.round(Number(rubles.replace(",", ".")) * 100);

// path is either a local file name or an absolute source URL: the importer
// keeps a link to the marketplace photo instead of downloading it.
const imageURL = (path: string) =>
  path.startsWith("http") ? path : `/uploads/${path}`;

export default function Products() {
  const [list, setList] = useState<Product[]>([]);
  const [edit, setEdit] = useState<Partial<Product> | null>(null);
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

  // Массовое действие применяется либо к отмеченным строкам, либо ко всему,
  // что показывает текущий фильтр: 20 000 строк галочками не отметить, поэтому
  // на сервер уезжает сам фильтр.
  const [fillSel, setFillSel] = useState<Selection | null>(null);
  const [fillTasks, setFillTasks] = useState<string[]>(["photos"]);

  // Задача одна на инстанс, поэтому и полоса, и кружки в строках берутся из
  // одного состояния; по её окончании перечитываем страницу.
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

  // Пока задача идёт, страница перечитывается: докачанные строки сами меняют
  // хотлинк на свою миниатюру.
  useEffect(() => {
    if (!job?.running) return;
    const id = setInterval(() => void reload(), 3000);
    return () => clearInterval(id);
  }, [job?.running, reload]);

  const runFill = async (tasks: string[]) => {
    if (!fillSel) return;
    setFillSel(null);
    setBulkMsg("");
    try {
      const r = await api.bulkFill({
        ids: fillSel.ids,
        all: fillSel.all,
        q: query,
        supplier: supplier ?? null,
        tasks,
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

  const save = async () => {
    if (!edit?.title) return;
    const p = { ...edit, price: toMinor(priceRub) };
    delete p.stock;
    if (stock !== null) p.stock = stock;
    // supplier всегда отправляем явно: поле есть в форме, и пустое значение —
    // осмысленный выбор «без поставщика», а не «не трогать».
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

  return (
    <div>
      <JobBar job={job} />

      {fillSel && (
        <Modal
          title={t("fillTitle")}
          onClose={() => setFillSel(null)}
          footer={
            <button
              className="btn"
              disabled={fillTasks.length === 0}
              onClick={() => void runFill(fillTasks)}
            >
              {t("fillRun")}
            </button>
          }
        >
          {/* Одна галочка сегодня, но список: поиск, перевод и заполнение
              описаний — та же работа над той же выборкой. */}
          <label className="flex cursor-pointer items-start gap-3">
            <input
              type="checkbox"
              className="mt-1"
              checked={fillTasks.includes("photos")}
              onChange={(e) => setFillTasks(e.target.checked ? ["photos"] : [])}
            />
            <span>
              <span className="font-semibold">{t("fillPhotos")}</span>
              <span className="hint mt-1 block">{t("fillPhotosHint")}</span>
            </span>
          </label>
        </Modal>
      )}

      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-bold">{t("heading")}</h1>
          <p className="hint mt-1">{t("rowHint")}</p>
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
                <input
                  className="field"
                  placeholder="CH-201"
                  value={edit.sku ?? ""}
                  onChange={(e) => setEdit({ ...edit, sku: e.target.value })}
                />
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
              <div className="min-w-40 flex-1">
                <label className="label">{t("labelCategory")}</label>
                <input
                  className="field"
                  list="product-categories"
                  placeholder="kuhnya"
                  value={edit.category ?? ""}
                  onChange={(e) =>
                    setEdit({ ...edit, category: e.target.value })
                  }
                />
                <datalist id="product-categories">
                  {categories.map((c) => (
                    <option key={c} value={c} />
                  ))}
                </datalist>
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
            </div>
            {edit.id ? (
              <div>
                <label className="label">{t("labelPhotos")}</label>
                <div className="flex flex-wrap items-center gap-3">
                  {edit.images?.map((im) => (
                    <span key={im.id} className="group relative">
                      <img
                        src={imageURL(im.path)}
                        alt=""
                        className="border-line h-20 w-20 rounded-lg border object-cover"
                      />
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
                  {/* Родной input file показывает «Не выбран ни один файл» и не
                    поддаётся стилизации — прячем его за подписью-кнопкой. */}
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
                {p.images?.[0] && (
                  <span className="relative inline-block h-10 w-10 shrink-0">
                    <img
                      src={imageURL(p.images[0].path)}
                      alt=""
                      className="h-10 w-10 rounded object-cover"
                    />
                    {inFlight.has(p.id) && (
                      <span className="absolute inset-0 flex items-center justify-center rounded bg-white/70">
                        <span className="border-brand h-4 w-4 animate-spin rounded-full border-2 border-t-transparent" />
                      </span>
                    )}
                  </span>
                )}
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
            render: (p) => `${toRubles(p.price)} ${sign}`,
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
            onClick: (sel) => setFillSel(sel),
          },
          {
            label: t("bulkDelete"),
            icon: <IconTrash />,
            danger: true,
            // Нет режима «удалить всё по фильтру»: одна кнопка, стирающая
            // каталог вместе с проиндексированными адресами, того не стоит.
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
