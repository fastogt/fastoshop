import { useRef, useState } from "react";
import { api, apiError, type Product } from "./api";
import { useT } from "./i18n";
import Modal from "./Modal";
import { toMinor, toRubles } from "./money";
import { imageURL, isRemoteImage, useSign } from "./shop";

const kText = {
  showOnStorefront: {
    ru: "Показывать на витрине",
    en: "Show on the storefront",
  },
  hiddenHint: {
    ru: "Скрытый товар пропадает из каталога, из карты сайта и не открывается по прямой ссылке. На публикацию в каналах это не влияет.",
    en: "A hidden product disappears from the catalogue and the sitemap, and its page stops opening. Channel publication is not affected.",
  },
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
    ru: "Длина × ширина × высота упаковки. По весу и габаритам считается доставка, поэтому пустое поле честнее нуля: незаполненный вес - это «неизвестно», а не «невесомый».",
    en: "Length × width × height of the parcel. Delivery is priced by weight and size, so an empty field is more honest than a zero: an unstated weight means “unknown”, not “weightless”.",
  },
  cardShop: { ru: "Витрина", en: "Storefront" },
  cardStock: { ru: "Цена и склад", en: "Price and stock" },
  cardPhotos: { ru: "Фото", en: "Photos" },
  cardChannels: { ru: "Для площадок", en: "For marketplaces" },
  paramAdd: { ru: "+ Свойство", en: "+ Property" },
  paramRemove: { ru: "Убрать", en: "Remove" },
  labelParams: { ru: "Характеристики", en: "Characteristics" },
  paramName: { ru: "Свойство", en: "Property" },
  paramValue: { ru: "Значение", en: "Value" },
  labelCategory: { ru: "Категория", en: "Category" },
  categoryPlaceholder: { ru: "Посуда/Кастрюли", en: "Cookware/Pots" },
  labelBrand: { ru: "Бренд", en: "Brand" },
  brandHint: {
    ru: "Производитель товара, а не поставщик. Уходит в разметку страницы и в товарные фиды: без бренда Google Merchant Center показывает карточку реже.",
    en: "The maker of the goods, not the supplier. It goes into the page markup and the product feeds: without a brand, Merchant Center shows the listing less often.",
  },
  categoryHint: {
    ru: "Косая черта задаёт вложенность: «Посуда/Кастрюли» - это страница «Кастрюли» внутри «Посуды». У каждого уровня своя страница на витрине.",
    en: 'A slash makes a level: "Cookware/Pots" is a Pots page inside Cookware. Every level gets a page of its own on the storefront.',
  },
  skuLocked: {
    ru: "Артикул связывает товар с выгрузкой поставщика - по нему загрузка находит, что обновлять. Чтобы изменить, сначала уберите поставщика.",
    en: "The article is what links this product to its supplier's feed - an import finds what to update by it. To change it, clear the supplier first.",
  },
  enrich: { ru: "Улучшить текст (AI)", en: "Improve the text (AI)" },
  enriching: { ru: "Пишем…", en: "Writing…" },
  enrichHint: {
    ru: "Название и описание перепишет модель - проверьте факты перед сохранением. За карточку отвечаете вы. Пока не нажали «Сохранить», в магазине ничего не изменилось.",
    en: "A model rewrites the name and the description - check the facts before saving. The card is your responsibility. Until you press Save, nothing in the shop has changed.",
  },
  labelDescription: { ru: "Описание", en: "Description" },
  descriptionPlaceholder: {
    ru: "Что это, из чего сделано, кому подойдёт - этот текст читают и покупатели, и поисковики.",
    en: "What it is, what it is made of, who it suits - this text is read by shoppers and search engines alike.",
  },
  labelPhotos: { ru: "Фотографии", en: "Photos" },
  addPhoto: { ru: "Добавить", en: "Add" },
  removePhoto: { ru: "Удалить фото", en: "Remove photo" },
  dragHint: {
    ru: "Фотографии можно перетаскивать: первая уходит в поиск, в каталог и в карточку канала.",
    en: "Photos can be dragged: the first one goes to search, to the catalogue and to a channel listing.",
  },
  photosHint: {
    ru: "JPEG, PNG или WebP, до 10 МБ. Первое фото попадает в поисковую выдачу и в карточку канала.",
    en: "JPEG, PNG or WebP, up to 10 MB. The first photo is what search results and the channel card show.",
  },
  remotePhoto: {
    ru: "Фото лежит на чужом сервере. Закроют его - витрина останется без картинки. «Забрать фото к себе» скачает его к нам.",
    en: "This photo lives on someone else’s server. The day it goes, the storefront loses the picture. “Bring the photos in” downloads it to us.",
  },
  labelSupplier: { ru: "Поставщик (группа)", en: "Supplier (group)" },
  supplierPlaceholder: { ru: "без поставщика", en: "no supplier" },
  supplierHint: {
    ru: "Чей это товар: только выгрузка этой группы будет обновлять его цену и остаток. Осторожно: если в выгрузке такого артикула нет, ближайшая загрузка обнулит остаток.",
    en: "Whose goods these are: only this group's feed updates the price and stock. Careful - if the feed has no such article, the next import zeroes the stock.",
  },
  editTitle: { ru: "Товар", en: "Product" },
  newTitle: { ru: "Новый товар", en: "New product" },
  photosAfterSave: {
    ru: "Фотографии можно добавить после сохранения.",
    en: "You can add photos once the product is saved.",
  },
  save: { ru: "Сохранить", en: "Save" },
  cancel: { ru: "Отмена", en: "Cancel" },
  failed: {
    ru: "Не получилось. Обновите страницу и попробуйте ещё раз.",
    en: "That did not work. Reload the page and try again.",
  },
};

// The card is one record shown in parts, so the tabs are a view state and not
// four forms: one Save, one request.
const kCardTabs = [
  "cardShop",
  "cardStock",
  "cardPhotos",
  "cardChannels",
] as const;
type CardTab = (typeof kCardTabs)[number];

// An empty field is "nobody said", which the server stores as NULL - not as a
// zero a delivery quote would take for a real measurement.
const numOrNull = (v: string): number | null => {
  const n = Number(v.trim());
  return v.trim() === "" || !Number.isFinite(n) || n <= 0 ? null : n;
};

export default function ProductCard({
  initial,
  suppliers,
  categories,
  brands,
  hasAIKey,
  onClose,
  onSaved,
  onReload,
}: {
  initial: Partial<Product>;
  suppliers: string[];
  categories: string[];
  brands: string[];
  // Whether the shop has an AdHunters key: the button is not offered without
  // one, because there is nothing to pay the rewriting with.
  hasAIKey: boolean;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onReload: () => Promise<void>;
}) {
  const t = useT(kText);
  const sign = useSign();
  const [edit, setEdit] = useState<Partial<Product>>(initial);
  const [cardTab, setCardTab] = useState<CardTab>("cardShop");
  // Index of the photo being dragged. A ref, not state: it changes on every
  // dragover and re-rendering the strip mid-drag drops the drag itself.
  const dragFrom = useRef<number | null>(null);
  const [enriching, setEnriching] = useState(false);
  const [enrichMsg, setEnrichMsg] = useState("");
  const [priceRub, setPriceRub] = useState(toRubles(initial.price ?? 0));
  // null = the stock field was never touched. Sending it means re-declaring the
  // physical stock: a form opened before a sale would resurrect sold units.
  const [stock, setStock] = useState<number | null>(null);

  // The draft lands straight in the form: the dialog is already a draft -
  // nothing reaches the database until Save, and closing the window undoes it.
  // Rows live in state as they are, blanks included: filtering on every
  // keystroke deleted the row whose name was being retyped, value and all. The
  // server drops nameless rows on save, so the screen and the record agree.
  const setParam = (i: number, name?: string, value?: string) =>
    setEdit((prev) => {
      const rows = [...(prev.params ?? [])];
      rows[i] = { name: name ?? rows[i].name, value: value ?? rows[i].value };
      return { ...prev, params: rows };
    });

  const addParam = () =>
    setEdit((prev) => ({
      ...prev,
      params: [...(prev.params ?? []), { name: "", value: "" }],
    }));

  const removeParam = (i: number) =>
    setEdit((prev) => ({
      ...prev,
      params: (prev.params ?? []).filter((_, n) => n !== i),
    }));

  const enrich = async () => {
    if (!edit.id) return;
    setEnrichMsg("");
    setEnriching(true);
    try {
      const d = await api.enrichProduct(edit.id);
      setEdit((prev) => ({
        ...prev,
        title: d.title,
        description: d.description,
        category: d.category || prev.category,
      }));
    } catch (e) {
      setEnrichMsg(apiError(e) ?? t("failed"));
    } finally {
      setEnriching(false);
    }
  };

  const save = async () => {
    if (!edit.title) return;
    const p = { ...edit, price: toMinor(priceRub) };
    delete p.stock;
    if (stock !== null) p.stock = stock;
    // supplier is always sent explicitly: the field is in the form, and an
    // empty value is a deliberate "no supplier" choice, not "leave as is".
    p.supplier = edit.supplier ?? "";
    if (edit.id) await api.updateProduct(edit.id, p);
    else await api.createProduct(p);
    onClose();
    await onSaved();
  };

  return (
    <Modal
      title={edit.id ? t("editTitle") : t("newTitle")}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={save}>
            {t("save")}
          </button>
          <button className="btn-ghost" onClick={onClose}>
            {t("cancel")}
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {/* Same tab markup as the admin header: a row of buttons with an
            underline on the active one. Wrapping, because four of them do
            not fit a 390px screen in one line. */}
        <div className="border-line -mt-2 flex flex-wrap gap-1 border-b">
          {kCardTabs.map((k) => (
            <button
              key={k}
              onClick={() => setCardTab(k)}
              className={
                "-mb-px border-b-2 px-3 py-2 text-sm font-semibold transition-colors " +
                (cardTab === k
                  ? "border-brand text-brand"
                  : "text-muted hover:text-ink border-transparent")
              }
            >
              {t(k)}
            </button>
          ))}
        </div>
        {cardTab === "cardShop" && (
          <>
            <div>
              <label className="label">{t("labelTitle")}</label>
              <input
                className="field"
                placeholder={t("titlePlaceholder")}
                value={edit.title ?? ""}
                onChange={(e) => setEdit({ ...edit, title: e.target.value })}
              />
            </div>
            {/* A category is a path, and paths are long: its own full-width row,
                not a quarter of a row next to the price. */}
            <div>
              <label className="label">{t("labelCategory")}</label>
              <input
                className="field"
                list="product-categories"
                placeholder={t("categoryPlaceholder")}
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
            <div>
              <label className="label">{t("labelBrand")}</label>
              <input
                className="field"
                list="product-brands"
                value={edit.brand ?? ""}
                onChange={(e) => setEdit({ ...edit, brand: e.target.value })}
              />
              <datalist id="product-brands">
                {brands.map((b) => (
                  <option key={b} value={b} />
                ))}
              </datalist>
              <p className="hint mt-1">{t("brandHint")}</p>
            </div>
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
          </>
        )}
        {cardTab === "cardStock" && (
          <>
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
              <p className="hint mt-1">{t("supplierHint")}</p>
            </div>
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
          </>
        )}
        {cardTab === "cardPhotos" && (
          <>
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
                          .then(() => onReload())
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
                      {isRemoteImage(im.path) && (
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
                      styling - we hide it behind a button-styled label. */}
                  <label className="btn-ghost border-line flex h-20 w-20 cursor-pointer flex-col items-center justify-center gap-1 border-2 border-dashed text-center text-xs">
                    <span className="text-lg leading-none">+</span>
                    {t("addPhoto")}
                    <input
                      type="file"
                      accept="image/jpeg,image/png,image/webp"
                      multiple
                      className="hidden"
                      onChange={async (e) => {
                        const files = Array.from(e.target.files ?? []);
                        e.target.value = "";
                        const id = edit.id;
                        if (!id) return;
                        // One at a time: position decides which photo is the
                        // main one, and parallel uploads would land in
                        // whatever order the server happened to finish.
                        for (const f of files)
                          setEdit(await api.uploadImage(id, f));
                      }}
                    />
                  </label>
                </div>
                <p className="hint mt-1">{t("photosHint")}</p>
              </div>
            ) : (
              <p className="hint">{t("photosAfterSave")}</p>
            )}
          </>
        )}
        {cardTab === "cardChannels" && (
          <>
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
            <div>
              <label className="label">{t("labelSize")}</label>
              <div className="flex items-center gap-2">
                {(["length_mm", "width_mm", "height_mm"] as const).map(
                  (k, i) => (
                    <div key={k} className="flex items-center gap-2">
                      {i > 0 && <span className="text-muted">×</span>}
                      <input
                        className="field w-24"
                        type="number"
                        min="0"
                        value={edit[k] ?? ""}
                        onChange={(e) =>
                          setEdit({
                            ...edit,
                            [k]: numOrNull(e.target.value),
                          })
                        }
                      />
                    </div>
                  ),
                )}
              </div>
              <p className="hint mt-1">{t("sizeHint")}</p>
            </div>
            <div>
              <label className="label">{t("labelParams")}</label>
              <div className="flex flex-col gap-2">
                {(edit.params ?? []).map((p, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <input
                      className="field w-1/3"
                      placeholder={t("paramName")}
                      value={p.name}
                      onChange={(e) => setParam(i, e.target.value, undefined)}
                    />
                    <input
                      className="field flex-1"
                      placeholder={t("paramValue")}
                      value={String(p.value ?? "")}
                      onChange={(e) => setParam(i, undefined, e.target.value)}
                    />
                    <button
                      className="btn-ghost px-2"
                      title={t("paramRemove")}
                      onClick={() => removeParam(i)}
                    >
                      ×
                    </button>
                  </div>
                ))}
                <div>
                  <button className="btn-ghost" onClick={addParam}>
                    {t("paramAdd")}
                  </button>
                </div>
              </div>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}
