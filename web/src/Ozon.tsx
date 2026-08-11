import { useCallback, useEffect, useState } from "react";
import {
  api,
  type OzonSettings,
  type OzonLinkResult,
  type OzonLinkPage,
  type OzonWarehouse,
  type OzonOrderPage,
  type OzonCandidatePage,
  type OzonPriceRule,
  type OzonCandidate,
  type OzonLink,
  type OzonOrder,
} from "./api";
import { useLang, useT } from "./i18n";
import { useSign } from "./shop";
import DataTable from "./DataTable";
import { IconDownload, IconUpload } from "./Icons";

// Prices travel in kopecks and are shown in rubles, same idiom as Products.tsx.
const toRubles = (minor: number) => (minor / 100).toFixed(2);
const toMinor = (rubles: string) =>
  Math.round(Number(rubles.replace(",", ".")) * 100);

// Ozon posting statuses live in the same dictionary under their raw codes. An
// unknown status is shown as is: the platform keeps adding new ones, and hiding
// them behind "—" is worse than showing the raw code.
const kText = {
  awaiting_registration: {
    ru: "ожидает регистрации",
    en: "awaiting registration",
  },
  acceptance_in_progress: { ru: "идёт приёмка", en: "acceptance in progress" },
  awaiting_approve: { ru: "ожидает подтверждения", en: "awaiting approval" },
  awaiting_packaging: { ru: "ожидает сборки", en: "awaiting packaging" },
  awaiting_deliver: { ru: "ожидает отгрузки", en: "awaiting shipment" },
  delivering: { ru: "в доставке", en: "in delivery" },
  delivered: { ru: "доставлено", en: "delivered" },
  arbitration: { ru: "спор", en: "dispute" },
  cancelled: { ru: "отменено", en: "cancelled" },

  errorPrefix: { ru: "Ошибка", en: "Error" },
  errorCheckKeys: { ru: "проверьте ключи", en: "check your keys" },

  intro: {
    ru: "Свяжите товары магазина с карточками кабинета Ozon, чтобы дальше управлять остатками и ценами из одного места.",
    en: "Link your shop products to their Ozon listings so you can manage stock and prices from one place.",
  },

  connection: { ru: "Подключение", en: "Connection" },
  fbsOnly: {
    ru: "Работаем по схеме FBS — товары лежат на вашем складе, и остатками управляет магазин. Товары на складе Ozon (FBO) синхронизировать нельзя: их остаток знает только площадка, и продать их с витрины вы не сможете.",
    en: "We work with FBS — the goods are in your warehouse and the shop manages the stock. Products stored at Ozon (FBO) cannot be synced: only the platform knows their stock, and you cannot ship them from your storefront.",
  },
  guide: {
    ru: "Кабинет продавца Ozon → Настройки → API-ключи → «Сгенерировать ключ». Скопируйте Client-Id и Api-Key.",
    en: "Ozon Seller account → Settings → API keys → “Generate key”. Copy the Client-Id and Api-Key.",
  },
  apiKeySaved: { ru: "сохранён", en: "saved" },
  warehouse: {
    ru: "Склад FBS (warehouse_id)",
    en: "FBS warehouse (warehouse_id)",
  },
  pickWarehouse: { ru: "— выберите склад —", en: "— pick a warehouse —" },
  loadWarehouses: { ru: "Загрузить склады", en: "Load warehouses" },
  noWarehouses: {
    ru: "Складов не нашлось — впишите id вручную",
    en: "No warehouses found — enter the id manually",
  },
  currency: { ru: "Валюта кабинета", en: "Account currency" },
  currencyHint: {
    ru: "Определяется по вашему юрлицу в кабинете и обновляется при проверке — в ней и уходят цены на площадку.",
    en: "Taken from your legal entity in the account and refreshed on every check — prices are sent to the marketplace in it.",
  },
  enabled: {
    ru: "Отправлять остатки и цены на Ozon",
    en: "Send stock and prices to Ozon",
  },
  save: { ru: "Сохранить", en: "Save" },
  saved: { ru: "Сохранено", en: "Saved" },
  check: { ru: "Проверить", en: "Check" },
  checked: {
    ru: "Кабинет {name}: товаров — {n}, валюта {cur}",
    en: "Account {name}: products — {n}, currency {cur}",
  },

  publication: { ru: "Публикация", en: "Publication" },
  publicationHint: {
    ru: "Отметьте товары, которые продаёте на Ozon. В канал уезжают только отмеченные — остальные живут на витрине и площадку не видят.",
    en: "Tick the products you sell on Ozon. Only ticked ones go to the channel; the rest live on the storefront and never reach the marketplace.",
  },
  searchProducts: {
    ru: "Поиск по названию или артикулу",
    en: "Search by title or article",
  },
  publish: { ru: "Опубликовать", en: "Publish" },
  unpublish: { ru: "Снять с публикации", en: "Unpublish" },
  noCandidates: {
    ru: "Товаров нет. Заведите их вручную или перенесите каталог на вкладке «Импорт».",
    en: "No products yet. Add them by hand or bring a catalogue over on the Import tab.",
  },
  colPublished: { ru: "В Ozon", en: "On Ozon" },
  yes: { ru: "да", en: "yes" },
  no: { ru: "нет", en: "no" },
  hiddenBadge: { ru: "скрыт с витрины", en: "hidden from storefront" },
  publishedResult: { ru: "Опубликовано: {n}", en: "Published: {n}" },
  unpublishedResult: { ru: "Снято: {n}", en: "Unpublished: {n}" },
  noCardTitle: {
    ru: "Нет карточки на Ozon",
    en: "No card on Ozon",
  },
  noCardHint: {
    ru: "Эти товары не нашлись в кабинете по артикулу. Заведите карточку на Ozon с тем же артикулом и повторите.",
    en: "These products had no article match in the cabinet. Create the card on Ozon with the same article and try again.",
  },
  zeroFailedTitle: {
    ru: "Не удалось обнулить остаток",
    en: "Could not zero the stock",
  },
  zeroFailedHint: {
    ru: "Связь сохранена намеренно: пока на площадке остаётся наш остаток, забывать про карточку нельзя — она продолжит продавать.",
    en: "The link is kept on purpose: while our stock is still live on the platform, forgetting the card would let it keep selling.",
  },
  linking: { ru: "Связывание", en: "Linking" },
  linkingHint: {
    ru: "Связываем по артикулу: артикул товара в магазине должен совпадать с offer_id карточки в кабинете Ozon — символ в символ.",
    en: "Matching goes by article: a product’s article in the shop must equal the offer_id of the Ozon listing, character for character.",
  },
  linkedNow: {
    ru: "Сейчас связано: {linked}, без связи: {unlinked}.",
    en: "Currently linked: {linked}, not linked: {unlinked}.",
  },
  linkByArticle: { ru: "Связать по артикулу", en: "Link by article" },
  linkedResult: { ru: "Связано: {n}", en: "Linked: {n}" },

  missingOnOzon: {
    ru: "Нет в кабинете Ozon",
    en: "Missing from the Ozon account",
  },
  missingOnOzonHint: {
    ru: "Эти товары магазина не нашлись по артикулу — заведите карточку на Ozon или поправьте артикул.",
    en: "These shop products had no article match — create the listing on Ozon or fix the article.",
  },
  articleOf: { ru: "(артикул: {sku})", en: "(article: {sku})" },
  articleEmpty: { ru: "не задан", en: "not set" },

  linkedProducts: { ru: "Связанные товары", en: "Linked products" },
  linkedProductsHint: {
    ru: "Цена на Ozon — ваша цена на площадке, отдельная от витринной. Пустая цена значит, что мы её не трогаем: на Ozon останется та, что стоит в кабинете.",
    en: "The Ozon price is your marketplace price, separate from the shop one. An empty price means we leave it alone: Ozon keeps whatever is set in the account.",
  },
  fillFromShop: {
    ru: "Заполнить из цены витрины +",
    en: "Fill from the shop price +",
  },
  fill: { ru: "Заполнить", en: "Fill" },
  ladder: { ru: "Лестница наценки", en: "Markup ladder" },
  ladderHint: {
    ru: "Один процент не работает на каталоге, где есть и товар за 30, и за 3000: на дешёвом он не отбивает комиссию площадки. Задайте полосы: до какой цены витрины какой множитель. Последняя строка «и выше» обязательна.",
    en: 'A single percentage does not work on a catalogue with both 30-ruble and 3000-ruble goods: on the cheap end it does not cover the platform fee. Set bands: up to which shelf price which multiplier. The final "and above" row is required.',
  },
  bandUpTo: { ru: "До цены", en: "Up to" },
  bandAbove: { ru: "и выше", en: "and above" },
  bandMultiplier: { ru: "Множитель", en: "Multiplier" },
  addBand: { ru: "+ Полоса", en: "+ Band" },
  removeBand: { ru: "Удалить", en: "Remove" },
  saveLadder: { ru: "Сохранить лестницу", en: "Save ladder" },
  applyLadder: { ru: "Применить к пустым ценам", en: "Apply to empty prices" },
  ladderSaved: { ru: "Лестница сохранена", en: "Ladder saved" },
  fillHint: {
    ru: "Заполняет только пустые цены, установленные не трогает.",
    en: "Only empty prices are filled; the ones you set stay as they are.",
  },
  filled: { ru: "Заполнено цен: {n}", en: "Prices filled: {n}" },

  colProduct: { ru: "Товар", en: "Product" },
  colArticle: { ru: "Артикул", en: "Article" },
  colStock: { ru: "Остаток", en: "Stock" },
  colShopPrice: { ru: "Цена витрины", en: "Shop price" },
  colOzonPrice: { ru: "Цена на Ozon", en: "Ozon price" },
  colStatus: { ru: "Статус", en: "Status" },
  productDeleted: { ru: "товар удалён", en: "product deleted" },
  rowStockError: { ru: "остаток: {err}", en: "stock: {err}" },
  rowPriceError: { ru: "цена: {err}", en: "price: {err}" },
  priceNotManaged: { ru: "ценой не управляем", en: "price not managed" },
  priceQueued: { ru: "цена ждёт отправки", en: "price queued to send" },
  priceSent: { ru: "отправлено {price} {cur}", en: "sent {price} {cur}" },

  back: { ru: "Назад", en: "Back" },
  forward: { ru: "Вперёд", en: "Next" },
  pageOf: { ru: "Страница {page} из {pages}", en: "Page {page} of {pages}" },

  extraOnOzon: {
    ru: "Есть в кабинете, нет в магазине",
    en: "In the account, not in the shop",
  },
  extraOnOzonHint: {
    ru: "Эти карточки Ozon не совпали ни с одним товаром — их можно перенести на вкладке «Импорт».",
    en: "These Ozon listings matched no product — you can bring them over on the Import tab.",
  },

  sync: { ru: "Синхронизация", en: "Sync" },
  syncHint: {
    ru: "Остатки и цены уезжают на Ozon сами: раз в пять минут и сразу после заказа или правки остатка в админке. Продажи на Ozon приезжают обратно тем же проходом и уменьшают остаток в магазине.",
    en: "Stock and prices go to Ozon on their own: every five minutes, and right after an order or a stock edit in the admin. Ozon sales come back on the same pass and reduce the shop stock.",
  },
  syncCounts: {
    ru: "Связано: {linked}. Остатки: ждёт отправки {pending}, с ошибкой {failed}. Цены: ждёт отправки {pricePending}, с ошибкой {priceFailed}.",
    en: "Linked: {linked}. Stock: queued {pending}, failed {failed}. Prices: queued {pricePending}, failed {priceFailed}.",
  },
  syncOff: { ru: " Отправка выключена.", en: " Sending is off." },
  pushNow: { ru: "Отправить сейчас", en: "Send now" },
  pushed: {
    ru: "Отправлено: {pushed}, с ошибкой: {failed}",
    en: "Sent: {pushed}, failed: {failed}",
  },
  kindStock: { ru: "остаток", en: "stock" },
  kindPrice: { ru: "цена", en: "price" },
  colWhat: { ru: "Что", en: "What" },
  colOurs: { ru: "У нас", en: "Ours" },
  colSent: { ru: "Отправлено", en: "Sent" },
  colError: { ru: "Ошибка", en: "Error" },

  sales: { ru: "Продажи на Ozon", en: "Ozon sales" },
  salesHint: {
    ru: "Заказы площадки: они списывают остаток в магазине, но в раздел «Заказы» и налоговый CSV не попадают — по ним отчитывается сам Ozon.",
    en: "Marketplace orders: they draw down shop stock, but never show up under Orders or in the tax CSV — Ozon reports them itself.",
  },
  salesTotal: { ru: "Всего: {n}", en: "Total: {n}" },
  salesOversold: { ru: ", с оверселлом: {n}", en: ", oversold: {n}" },
  salesUnresolved: {
    ru: ", с несопоставленными позициями: {n}",
    en: ", with unmatched items: {n}",
  },
  pollError: {
    ru: "Опрос заказов Ozon не прошёл: {err}. Пока он не пройдёт, остатки на площадку не отправляются.",
    en: "Polling Ozon orders failed: {err}. Until it succeeds, stock is not sent to the marketplace.",
  },
  noSales: { ru: "Продаж на Ozon пока не было.", en: "No Ozon sales yet." },
  colPosting: { ru: "Отправление", en: "Shipment" },
  colDate: { ru: "Дата", en: "Date" },
  colItems: { ru: "Состав", en: "Items" },
  itemUnmatched: {
    ru: "{offer} × {qty} — не смогли сопоставить товар",
    en: "{offer} × {qty} — could not match a product",
  },
  oversold: {
    ru: "продано больше, чем было у нас",
    en: "sold more than we had in stock",
  },
};

export default function Ozon() {
  const t = useT(kText);
  const sign = useSign();
  const lang = useLang();
  const [s, setS] = useState<OzonSettings | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [msg, setMsg] = useState("");
  const [linkMsg, setLinkMsg] = useState("");
  const [result, setResult] = useState<OzonLinkResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [warehouses, setWarehouses] = useState<OzonWarehouse[] | null>(null);
  const [stockMsg, setStockMsg] = useState("");
  const [orders, setOrders] = useState<OzonOrderPage | null>(null);
  const [page, setPage] = useState(1);
  const [links, setLinks] = useState<OzonLinkPage | null>(null);
  const [linkPage, setLinkPage] = useState(1);
  // Draft of the price field: an empty draft means the field was not touched,
  // and the row shows what is stored.
  const [priceDraft, setPriceDraft] = useState<Record<number, string>>({});
  const [markup, setMarkup] = useState("25");
  const [rules, setRules] = useState<OzonPriceRule[]>([]);
  const [ladderMsg, setLadderMsg] = useState("");
  const [priceMsg, setPriceMsg] = useState("");
  const [candidates, setCandidates] = useState<OzonCandidatePage | null>(null);
  const [candPage, setCandPage] = useState(1);
  const [candSearch, setCandSearch] = useState("");
  const [candQuery, setCandQuery] = useState("");
  const [pubMsg, setPubMsg] = useState("");
  const [noCard, setNoCard] = useState<{ id: number; sku: string }[]>([]);
  const [zeroFailed, setZeroFailed] = useState<
    { id: number; sku: string; title: string }[]
  >([]);

  const loadLinks = useCallback(
    () => api.ozonLinks(linkPage).then(setLinks),
    [linkPage],
  );

  const loadCandidates = useCallback(
    () => api.ozonCandidates(candPage, candQuery).then(setCandidates),
    [candPage, candQuery],
  );

  useEffect(() => {
    api.ozonSettings().then(setS);
    api.ozonPriceRules().then((r) => setRules(r.rules));
  }, []);
  useEffect(() => {
    api.ozonOrders(page).then(setOrders);
  }, [page]);
  useEffect(() => {
    loadLinks();
  }, [loadLinks]);
  useEffect(() => {
    loadCandidates();
  }, [loadCandidates]);
  // Typing in the picker must not hit the API on every keystroke.
  useEffect(() => {
    const timer = setTimeout(() => {
      setCandPage(1);
      setCandQuery(candSearch.trim());
    }, 300);
    return () => clearTimeout(timer);
  }, [candSearch]);
  if (!s) return null;

  const fail = (e: unknown) =>
    t("errorPrefix") +
    ": " +
    ((e as { response?: { data?: { error?: { message?: string } } } }).response
      ?.data?.error?.message ?? t("errorCheckKeys"));

  const isError = (m: string) => m.startsWith(t("errorPrefix"));

  // Both push kinds in one table: the owner cares about the row that did not
  // reach Ozon, not about which of the two calls carried it.
  const syncErrors = [
    ...s.stock_errors.map((e) => ({
      key: `stock-${e.product_id}`,
      offer_id: e.offer_id,
      kind: t("kindStock"),
      want: String(e.stock),
      pushed: e.pushed < 0 ? "—" : String(e.pushed),
      error: e.error,
    })),
    ...s.price_errors.map((e) => ({
      key: `price-${e.product_id}`,
      offer_id: e.offer_id,
      kind: t("kindPrice"),
      want: `${toRubles(e.price)} ${s.currency}`,
      pushed: e.pushed < 0 ? "—" : `${toRubles(e.pushed)} ${s.currency}`,
      error: e.error,
    })),
  ];

  // Saving on blur: a per-row button would double the width of the table, and
  // leaving the tab without saving what was typed is worse than one extra
  // request on a field the owner merely tabbed through.
  const savePrice = async (productId: number, current: number) => {
    const draft = priceDraft[productId];
    if (draft === undefined) return;
    setPriceDraft((d) => {
      const next = { ...d };
      delete next[productId];
      return next;
    });
    const minor = toMinor(draft);
    if (!Number.isFinite(minor) || minor < 0 || minor === current) return;
    setPriceMsg("");
    try {
      await api.ozonSetPrice(productId, minor);
      await loadLinks();
      setS(await api.ozonSettings());
    } catch (e) {
      setPriceMsg(fail(e));
    }
  };

  const saveLadder = async () => {
    setBusy(true);
    setLadderMsg("");
    try {
      const r = await api.ozonSetPriceRules(rules);
      setRules(r.rules);
      setLadderMsg(t("ladderSaved"));
    } catch (e) {
      setLadderMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const applyLadder = async () => {
    setBusy(true);
    setLadderMsg("");
    try {
      const r = await api.ozonFillByRules();
      setLadderMsg(t("filled", { n: r.filled }));
      await loadLinks();
      setS(await api.ozonSettings());
    } catch (e) {
      setLadderMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const fillPrices = async () => {
    setBusy(true);
    setPriceMsg("");
    try {
      const r = await api.ozonFillPrices(Math.round(Number(markup) * 100));
      setPriceMsg(t("filled", { n: r.filled }));
      await loadLinks();
      setS(await api.ozonSettings());
    } catch (e) {
      setPriceMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const save = async () => {
    setBusy(true);
    setMsg("");
    try {
      const body: Record<string, unknown> = { ...s };
      if (apiKey) body.api_key = apiKey;
      setS(await api.saveOzonSettings(body));
      setApiKey("");
      setMsg(t("saved"));
    } catch (e) {
      setMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const check = async () => {
    setBusy(true);
    setMsg("");
    try {
      const r = await api.ozonCheck();
      setMsg(
        t("checked", {
          name: r.legal_name || "",
          n: r.total,
          cur: r.currency || s.currency,
        }).trim(),
      );
      if (r.currency) setS({ ...s, currency: r.currency });
    } catch (e) {
      setMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const afterPublishChange = async () => {
    setS(await api.ozonSettings());
    await Promise.all([loadLinks(), loadCandidates()]);
  };

  const publish = async (ids: number[]) => {
    setBusy(true);
    setPubMsg("");
    setNoCard([]);
    setZeroFailed([]);
    try {
      const r = await api.ozonPublish(ids);
      setPubMsg(t("publishedResult", { n: r.published }));
      setNoCard(r.no_card);
      await afterPublishChange();
    } catch (e) {
      setPubMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const unpublish = async (ids: number[]) => {
    setBusy(true);
    setPubMsg("");
    setNoCard([]);
    setZeroFailed([]);
    try {
      const r = await api.ozonUnpublish(ids);
      setPubMsg(t("unpublishedResult", { n: r.unpublished }));
      setZeroFailed(r.failed);
      await afterPublishChange();
    } catch (e) {
      setPubMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const link = async () => {
    setBusy(true);
    setLinkMsg("");
    setResult(null);
    try {
      const r = await api.ozonLink();
      setResult(r);
      setLinkMsg(t("linkedResult", { n: r.linked }));
      setS(await api.ozonSettings());
      await loadLinks();
    } catch (e) {
      setLinkMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  // The warehouse list is a convenience, not an obligation: if the method
  // answers in a way we did not expect, the field stays manual and the owner
  // sees why.
  const loadWarehouses = async () => {
    setBusy(true);
    setMsg("");
    try {
      const list = await api.ozonWarehouses();
      setWarehouses(list);
      if (list.length === 0) setMsg(t("noWarehouses"));
    } catch (e) {
      setWarehouses(null);
      setMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const push = async () => {
    setBusy(true);
    setStockMsg("");
    try {
      const r = await api.ozonPush();
      setStockMsg(t("pushed", { pushed: r.pushed, failed: r.failed }));
      setS(await api.ozonSettings());
      setOrders(await api.ozonOrders(page));
      await loadLinks();
    } catch (e) {
      setStockMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="form-page">
      <div>
        <h1 className="text-xl font-bold">Ozon</h1>
        <p className="hint mt-1">{t("intro")}</p>
      </div>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("connection")}</h2>
          <p className="hint">{t("fbsOnly")}</p>
          <p className="hint mt-1">{t("guide")}</p>
        </div>
        <div>
          <label className="label">Client-Id</label>
          <input
            className="field"
            // Браузер принимает пару «текст + пароль» за форму входа и
            // подставляет сюда сохранённые логин и пароль от админки. Сохранив
            // их, владелец положил бы свой пароль в базу как ключ Ozon.
            name="ozon-client-id"
            autoComplete="off"
            value={s.client_id}
            onChange={(e) => setS({ ...s, client_id: e.target.value })}
          />
        </div>
        <div>
          <label className="label">Api-Key</label>
          <input
            className="field"
            type="password"
            name="ozon-api-key"
            autoComplete="new-password"
            placeholder={s.api_key_set ? t("apiKeySaved") : ""}
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
          />
        </div>
        <div>
          <label className="label">{t("warehouse")}</label>
          <div className="flex items-center gap-3">
            {warehouses && warehouses.length > 0 ? (
              <select
                className="field"
                name="ozon-warehouse"
                autoComplete="off"
                value={s.warehouse_id}
                onChange={(e) => setS({ ...s, warehouse_id: e.target.value })}
              >
                <option value="">{t("pickWarehouse")}</option>
                {warehouses.map((wh) => (
                  <option key={wh.id} value={wh.id}>
                    {wh.name} ({wh.id})
                  </option>
                ))}
              </select>
            ) : (
              <input
                className="field"
                name="ozon-warehouse"
                autoComplete="off"
                value={s.warehouse_id}
                onChange={(e) => setS({ ...s, warehouse_id: e.target.value })}
              />
            )}
            <button
              className="btn-ghost"
              disabled={busy}
              onClick={loadWarehouses}
            >
              {t("loadWarehouses")}
            </button>
          </div>
        </div>
        <div>
          <label className="label">{t("currency")}</label>
          <p className="font-semibold">{s.currency}</p>
          <p className="hint">{t("currencyHint")}</p>
        </div>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={s.enabled}
            onChange={(e) => setS({ ...s, enabled: e.target.checked })}
          />
          <span>{t("enabled")}</span>
        </label>
        <div className="flex items-center gap-3">
          <button className="btn" disabled={busy} onClick={save}>
            {t("save")}
          </button>
          <button className="btn-ghost" disabled={busy} onClick={check}>
            {t("check")}
          </button>
        </div>
        {msg && (
          <p className={isError(msg) ? "text-red-600" : "text-green-700"}>
            {msg}
          </p>
        )}
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("publication")}</h2>
          <p className="hint">{t("publicationHint")}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <input
            className="field w-64"
            placeholder={t("searchProducts")}
            value={candSearch}
            onChange={(e) => setCandSearch(e.target.value)}
          />
        </div>

        <DataTable<OzonCandidate>
          columns={[
            {
              key: "title",
              label: t("colProduct"),
              render: (p) => (
                <>
                  {p.title}
                  {p.hidden && (
                    <span className="text-muted ml-2 text-xs">
                      {t("hiddenBadge")}
                    </span>
                  )}
                </>
              ),
            },
            {
              key: "sku",
              label: t("colArticle"),
              hideMobile: true,
              render: (p) => p.sku || "—",
            },
            { key: "stock", label: t("colStock"), render: (p) => p.stock },
            {
              key: "published",
              label: t("colPublished"),
              render: (p) =>
                p.published ? (
                  t("yes")
                ) : (
                  <span className="text-muted">{t("no")}</span>
                ),
            },
          ]}
          rows={candidates?.products ?? []}
          rowId={(p) => p.product_id}
          total={candidates?.total ?? 0}
          page={candPage}
          pageSize={candidates?.page_size ?? 100}
          onPage={setCandPage}
          selectable
          // Публикация тянет список карточек кабинета и сверяет артикулы, поэтому
          // работает по явному списку; «всё по фильтру» здесь означало бы
          // безостановочный проход по каталогу на стороне площадки.
          allowAll={false}
          bulkActions={[
            {
              label: t("publish"),
              icon: <IconUpload />,
              idsOnly: true,
              onClick: (sel) => publish(sel.ids),
            },
            {
              label: t("unpublish"),
              icon: <IconDownload />,
              danger: true,
              idsOnly: true,
              onClick: (sel) => unpublish(sel.ids),
            },
          ]}
          emptyTitle={t("noCandidates")}
        />

        {pubMsg && (
          <p className={isError(pubMsg) ? "text-red-600" : "text-green-700"}>
            {pubMsg}
          </p>
        )}

        {noCard.length > 0 && (
          <div>
            <h3 className="font-semibold">{t("noCardTitle")}</h3>
            <p className="hint">{t("noCardHint")}</p>
            <ul className="mt-2 flex flex-col gap-1">
              {noCard.map((p) => (
                <li key={p.id} className="text-sm">
                  {p.sku || t("articleEmpty")}
                </li>
              ))}
            </ul>
          </div>
        )}

        {zeroFailed.length > 0 && (
          <div>
            <h3 className="font-semibold text-red-600">
              {t("zeroFailedTitle")}
            </h3>
            <p className="hint">{t("zeroFailedHint")}</p>
            <ul className="mt-2 flex flex-col gap-1">
              {zeroFailed.map((p) => (
                <li key={p.id} className="text-sm">
                  {p.sku}: {p.title}
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("linking")}</h2>
          <p className="hint">{t("linkingHint")}</p>
          <p className="hint mt-1">
            {t("linkedNow", { linked: s.linked, unlinked: s.unlinked })}
          </p>
        </div>
        <div>
          <button className="btn" disabled={busy} onClick={link}>
            {t("linkByArticle")}
          </button>
        </div>

        {linkMsg && (
          <p className={isError(linkMsg) ? "text-red-600" : "text-green-700"}>
            {linkMsg}
          </p>
        )}

        {result && result.unlinked_products.length > 0 && (
          <div>
            <h3 className="font-semibold">{t("missingOnOzon")}</h3>
            <p className="hint">{t("missingOnOzonHint")}</p>
            <ul className="mt-2 flex flex-col gap-1">
              {result.unlinked_products.map((p) => (
                <li key={p.id} className="text-sm">
                  {p.title}{" "}
                  <span className="text-muted">
                    {t("articleOf", { sku: p.sku || t("articleEmpty") })}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}

        {links && links.links.length > 0 && (
          <div className="flex flex-col gap-3">
            <div>
              <h3 className="font-semibold">{t("linkedProducts")}</h3>
              <p className="hint">{t("linkedProductsHint")}</p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <span className="hint">{t("fillFromShop")}</span>
              <input
                className="field w-20"
                value={markup}
                onChange={(e) => setMarkup(e.target.value)}
              />
              <span className="hint">%</span>
              <button
                className="btn-ghost"
                disabled={busy}
                onClick={fillPrices}
              >
                {t("fill")}
              </button>
              <span className="hint">{t("fillHint")}</span>
            </div>

            <div className="border-line flex flex-col gap-3 border-t pt-4">
              <div>
                <h3 className="font-semibold">{t("ladder")}</h3>
                <p className="hint">{t("ladderHint")}</p>
              </div>
              <div className="flex flex-col gap-2">
                {rules.map((rule, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <span className="hint w-20">{t("bandUpTo")}</span>
                    {rule.up_to === 0 ? (
                      <span className="w-28 font-semibold">
                        {t("bandAbove")}
                      </span>
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
                                    Number(e.target.value.replace(",", ".")) ||
                                    0,
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
              </div>
              <div className="flex flex-wrap items-center gap-3">
                <button
                  className="btn-ghost"
                  onClick={() =>
                    setRules([
                      ...rules.filter((r) => r.up_to !== 0),
                      { up_to: 100000, multiplier: 2 },
                      ...(rules.some((r) => r.up_to === 0)
                        ? rules.filter((r) => r.up_to === 0)
                        : [{ up_to: 0, multiplier: 1.5 }]),
                    ])
                  }
                >
                  {t("addBand")}
                </button>
                <button className="btn" disabled={busy} onClick={saveLadder}>
                  {t("saveLadder")}
                </button>
                <button
                  className="btn-ghost"
                  disabled={busy || rules.length === 0}
                  onClick={applyLadder}
                >
                  {t("applyLadder")}
                </button>
                {ladderMsg && (
                  <span
                    className={
                      isError(ladderMsg) ? "text-red-600" : "text-green-700"
                    }
                  >
                    {ladderMsg}
                  </span>
                )}
              </div>
            </div>
            <DataTable<OzonLink>
              columns={[
                {
                  key: "title",
                  label: t("colProduct"),
                  render: (l) =>
                    l.title || (
                      <span className="text-muted">{t("productDeleted")}</span>
                    ),
                },
                {
                  key: "offer_id",
                  label: t("colArticle"),
                  hideMobile: true,
                  render: (l) => l.offer_id,
                },
                { key: "stock", label: t("colStock"), render: (l) => l.stock },
                {
                  key: "shop_price",
                  label: t("colShopPrice"),
                  hideMobile: true,
                  render: (l) => `${toRubles(l.shop_price)} ${sign}`,
                },
                {
                  key: "price",
                  label: t("colOzonPrice"),
                  render: (l) => (
                    <input
                      className="field w-28"
                      value={priceDraft[l.product_id] ?? toRubles(l.price)}
                      onChange={(e) =>
                        setPriceDraft({
                          ...priceDraft,
                          [l.product_id]: e.target.value,
                        })
                      }
                      onBlur={() => savePrice(l.product_id, l.price)}
                    />
                  ),
                },
                {
                  key: "status",
                  label: t("colStatus"),
                  render: (l) => (
                    <>
                      {l.stock_error && (
                        <div className="text-red-600">
                          {t("rowStockError", { err: l.stock_error })}
                        </div>
                      )}
                      {l.price_error && (
                        <div className="text-red-600">
                          {t("rowPriceError", { err: l.price_error })}
                        </div>
                      )}
                      {!l.stock_error &&
                        !l.price_error &&
                        (l.price === 0 ? (
                          <span className="text-muted">
                            {t("priceNotManaged")}
                          </span>
                        ) : (
                          <span className="text-muted">
                            {l.price_pushed < 0
                              ? t("priceQueued")
                              : t("priceSent", {
                                  price: toRubles(l.price_pushed),
                                  cur: s.currency,
                                })}
                          </span>
                        ))}
                    </>
                  ),
                },
              ]}
              rows={links.links}
              rowId={(l) => l.product_id}
              total={links.total}
              page={linkPage}
              pageSize={links.page_size}
              onPage={setLinkPage}
              emptyTitle={t("linkedProducts")}
            />
            {priceMsg && (
              <p
                className={
                  isError(priceMsg) ? "text-red-600" : "text-green-700"
                }
              >
                {priceMsg}
              </p>
            )}
          </div>
        )}

        {result && result.unlinked_offers.length > 0 && (
          <div>
            <h3 className="font-semibold">{t("extraOnOzon")}</h3>
            <p className="hint">{t("extraOnOzonHint")}</p>
            <ul className="mt-2 flex flex-col gap-1">
              {result.unlinked_offers.map((o) => (
                <li key={o} className="text-sm">
                  {o}
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("sync")}</h2>
          <p className="hint">{t("syncHint")}</p>
          <p className="hint mt-1">
            {t("syncCounts", {
              linked: s.linked,
              pending: s.pending,
              failed: s.failed,
              pricePending: s.price_pending,
              priceFailed: s.price_failed,
            })}
            {!s.enabled && t("syncOff")}
          </p>
        </div>
        <div>
          <button className="btn" disabled={busy} onClick={push}>
            {t("pushNow")}
          </button>
        </div>

        {stockMsg && (
          <p className={isError(stockMsg) ? "text-red-600" : "text-green-700"}>
            {stockMsg}
          </p>
        )}

        {syncErrors.length > 0 && (
          <DataTable<(typeof syncErrors)[number]>
            columns={[
              {
                key: "offer_id",
                label: t("colArticle"),
                render: (e) => e.offer_id,
              },
              { key: "kind", label: t("colWhat"), render: (e) => e.kind },
              { key: "want", label: t("colOurs"), render: (e) => e.want },
              {
                key: "pushed",
                label: t("colSent"),
                hideMobile: true,
                render: (e) => e.pushed,
              },
              {
                key: "error",
                label: t("colError"),
                render: (e) => <span className="text-red-600">{e.error}</span>,
              },
            ]}
            rows={syncErrors}
            rowId={(e) => e.key}
            total={syncErrors.length}
            page={1}
            pageSize={syncErrors.length}
            onPage={() => {}}
            emptyTitle=""
          />
        )}
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("sales")}</h2>
          <p className="hint">{t("salesHint")}</p>
          <p className="hint mt-1">
            {t("salesTotal", { n: s.orders_total })}
            {s.orders_oversold > 0 &&
              t("salesOversold", { n: s.orders_oversold })}
            {s.orders_unresolved > 0 &&
              t("salesUnresolved", { n: s.orders_unresolved })}
            .
          </p>
        </div>

        {s.poll_error && (
          <p className="text-red-600">
            {t("pollError", { err: s.poll_error })}
          </p>
        )}

        <DataTable<OzonOrder>
          columns={[
            {
              key: "posting_number",
              label: t("colPosting"),
              render: (o) => o.posting_number,
            },
            {
              key: "created_at",
              label: t("colDate"),
              hideMobile: true,
              render: (o) => new Date(o.created_at).toLocaleDateString(lang),
            },
            {
              key: "status",
              label: t("colStatus"),
              render: (o) =>
                o.status in kText
                  ? t(o.status as keyof typeof kText)
                  : o.status,
            },
            {
              key: "items",
              label: t("colItems"),
              render: (o) => (
                <>
                  <ul className="flex flex-col gap-1">
                    {o.items.map((it, i) => (
                      <li key={`${it.offer_id}-${i}`}>
                        {it.product_id === null ? (
                          <span className="text-red-600">
                            {t("itemUnmatched", {
                              offer: it.offer_id,
                              qty: it.qty,
                            })}
                          </span>
                        ) : (
                          <span>
                            {it.title} × {it.qty}
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                  {o.oversold && (
                    <span className="text-red-600">{t("oversold")}</span>
                  )}
                </>
              ),
            },
          ]}
          rows={orders?.orders ?? []}
          rowId={(o) => o.posting_number}
          total={orders?.total ?? 0}
          page={page}
          pageSize={orders?.page_size ?? 50}
          onPage={setPage}
          emptyTitle={t("noSales")}
        />
      </section>
    </div>
  );
}
