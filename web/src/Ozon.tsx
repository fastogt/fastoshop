import { useCallback, useEffect, useMemo, useState } from "react";
import {
  api,
  apiError,
  type OzonSettings,
  type OzonLinkPage,
  type OzonWarehouse,
  type OzonOrderPage,
  type OzonCandidatePage,
  type PriceRule,
  type OzonCandidate,
  type CabinetState,
  type OzonLink,
  type OzonOrder,
  type CandidateView,
} from "./api";
import { useLang, useT } from "./i18n";
import { toRubles, toMinor } from "./money";
import { useSign } from "./shop";
import DataTable from "./DataTable";
import { IconDownload, IconUpload } from "./Icons";

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
  // Why one field and not a choice per product: a product carries a single
  // stock figure, so the same number sent to two warehouses would be the same
  // goods promised twice.
  warehouseHint: {
    ru: "Складов в кабинете может быть несколько — остатки уезжают на этот. У товара один остаток, разделить его между складами магазин не умеет.",
    en: "A cabinet may hold several warehouses — stock travels to this one. A product carries a single stock figure, and the shop cannot split it between warehouses.",
  },
  pickWarehouse: { ru: "— выберите склад —", en: "— pick a warehouse —" },
  loadWarehouses: { ru: "Загрузить склады", en: "Load warehouses" },
  noWarehouses: {
    ru: "Складов не нашлось — впишите id вручную",
    en: "No warehouses found — enter the id manually",
  },
  // The cabinet's currency is not ours to keep: one shop is one legal entity is
  // one money. When the check disagrees with the shop, the shop is what is wrong.
  currencyMismatch: {
    ru: "Кабинет торгует в {cabinet}, а магазин — в {shop}. Цены уедут в валюте магазина и площадка их отобьёт. Поправьте валюту в Профиле.",
    en: "The cabinet trades in {cabinet}, the shop in {shop}. Prices travel in the shop's currency and the platform will refuse them. Fix the currency in Profile.",
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
  cabinetSummary: {
    // Only the cabinet's own card count: the three states below it are on the
    // buttons, where they belong — the owner is choosing what to look at, and a
    // sentence repeating the choice word for word is noise between them.
    ru: "В кабинете карточек: {cards}.",
    en: "Cards in the cabinet: {cards}.",
  },
  cabinetOrphans: {
    ru: "Ещё {n} карточек на площадке не совпали ни с одним товаром.",
    en: "Another {n} cards on the platform matched no product of yours.",
  },
  stateReady: { ru: "можно связать", en: "ready to link" },
  stateNoCard: { ru: "нет карточки", en: "no card" },
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
  colPublished: { ru: "На площадке", en: "On the platform" },
  yes: { ru: "да", en: "yes" },
  no: { ru: "нет", en: "no" },
  hiddenBadge: { ru: "скрыт с витрины", en: "hidden from storefront" },
  publishedResult: { ru: "Опубликовано: {n}", en: "Published: {n}" },
  // Publishing writes the link, but stock travels only from a warehouse. An
  // owner who never picked one sees "published" and then nothing happens on the
  // platform, with no error anywhere: the worker skips stocks in silence.
  noWarehouse: {
    ru: " Остатки не поедут: не задан склад FBS. Выберите его в настройках выше, а если список пуст — заведите склад в кабинете Ozon: через API склады не создаются.",
    en: " Stock will not travel: no FBS warehouse is set. Pick one in the settings above, and if the list is empty, create a warehouse in the Ozon cabinet — the API does not create them.",
  },
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

  viewReady: { ru: "Можно связать", en: "Ready to link" },
  viewLinked: { ru: "Связано", en: "Linked" },
  viewNoCard: { ru: "Нет карточки", en: "No card" },
  viewAll: { ru: "Все товары", en: "All products" },
  orphanList: {
    ru: "Карточки в кабинете, которым не нашлось товара: {list}",
    en: "Cards in the cabinet with no product of ours: {list}",
  },
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

  extraOnOzon: {
    ru: "Есть в кабинете, нет в магазине",
    en: "In the account, not in the shop",
  },
  extraOnOzonMore: {
    ru: "…и ещё {n}",
    en: "…and {n} more",
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
  const [rules, setRules] = useState<PriceRule[]>([]);
  const [ladderMsg, setLadderMsg] = useState("");
  const [priceMsg, setPriceMsg] = useState("");
  const [candidates, setCandidates] = useState<OzonCandidatePage | null>(null);
  const [candPage, setCandPage] = useState(1);
  const [candSearch, setCandSearch] = useState("");
  const [candQuery, setCandQuery] = useState("");
  // Which slice the table shows. The tab opens on the only state with an action
  // attached: on a live shop the catalogue is 24 000 rows and the cabinet holds
  // a few dozen cards, so "everything" was a wall the owner had to scroll past
  // to find the seven products a button could do anything with.
  const [view, setView] = useState<CandidateView["kind"] | null>(null);
  const [pubMsg, setPubMsg] = useState("");
  const [noCard, setNoCard] = useState<{ id: number; sku: string }[]>([]);
  // Asked once when the tab opens, and again after publishing changed the
  // answer. Deliberately not part of the paged candidates call: that one runs
  // per page of a hundred rows and would re-read the cabinet every time.
  const [cabinet, setCabinet] = useState<CabinetState | null>(null);
  const [zeroFailed, setZeroFailed] = useState<
    { id: number; sku: string; title: string }[]
  >([]);

  const loadLinks = useCallback(
    () => api.ozonLinks(linkPage).then(setLinks),
    [linkPage],
  );

  // Open on the pile with a button attached; fall back to what is already
  // linked, and only then to everything — a shop with no cabinet answer at all
  // still gets the table it had before this existed.
  const defaultView: CandidateView["kind"] = !cabinet
    ? "all"
    : cabinet.ready > 0
      ? "ready"
      : cabinet.linked > 0
        ? "linked"
        : "all";

  const readyIDs = useMemo(() => cabinet?.ready_ids ?? [], [cabinet]);

  const loadCandidates = useCallback(
    () =>
      api
        .ozonCandidates(candPage, candQuery, {
          kind: view ?? defaultView,
          readyIDs,
        })
        .then(setCandidates),
    [candPage, candQuery, view, defaultView, readyIDs],
  );

  const ready = useMemo(() => new Set(cabinet?.ready_ids ?? []), [cabinet]);

  const loadCabinet = useCallback(
    // A shop with no keys, or a platform that will not answer, simply gets no
    // summary — the table worked before this existed and must keep working.
    () =>
      api
        .ozonCabinet()
        .then(setCabinet)
        .catch(() => setCabinet(null)),
    [],
  );

  useEffect(() => {
    api.ozonSettings().then(setS);
    api.ozonPriceRules().then((r) => setRules(r.rules));
    loadCabinet();
  }, [loadCabinet]);
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
    t("errorPrefix") + ": " + (apiError(e) ?? t("errorCheckKeys"));

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
      const checked = t("checked", {
        name: r.legal_name || "",
        n: r.total,
        cur: r.currency || s.currency,
      }).trim();
      const clash =
        r.currency && s.currency && r.currency !== s.currency
          ? " " +
            t("currencyMismatch", { cabinet: r.currency, shop: s.currency })
          : "";
      setMsg(checked + clash);
    } catch (e) {
      setMsg(fail(e));
    } finally {
      setBusy(false);
    }
  };

  const afterPublishChange = async () => {
    setS(await api.ozonSettings());
    await Promise.all([loadLinks(), loadCandidates(), loadCabinet()]);
  };

  const publish = async (ids: number[]) => {
    setBusy(true);
    setPubMsg("");
    setNoCard([]);
    setZeroFailed([]);
    try {
      const r = await api.ozonPublish(ids);
      setPubMsg(
        t("publishedResult", { n: r.published }) +
          (r.published > 0 && !s.warehouse_id ? t("noWarehouse") : ""),
      );
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
    <div className="page flex flex-col gap-6">
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
            // The browser mistakes a "text + password" pair for a login form
            // and autofills the saved admin login and password here. Saving
            // them, the owner would put their password into the DB as the Ozon
            // key.
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
          <p className="hint">{t("warehouseHint")}</p>
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
          {cabinet && (
            <p className="hint mt-1">
              {t("cabinetSummary", { cards: cabinet.cards })}
            </p>
          )}
        </div>
        {cabinet && (
          // The counts sit on the buttons rather than in a sentence above them:
          // the owner is choosing what to look at, and the size of each pile is
          // the whole basis for that choice.
          <div className="flex flex-wrap items-center gap-2">
            {(
              [
                ["ready", t("viewReady"), cabinet.ready],
                ["linked", t("viewLinked"), cabinet.linked],
                ["nocard", t("viewNoCard"), cabinet.no_card],
                ["all", t("viewAll"), cabinet.products],
              ] as const
            ).map(([kind, label, n]) => (
              <button
                key={kind}
                onClick={() => {
                  setView(kind);
                  setCandPage(1);
                }}
                className={
                  "rounded-full border px-3 py-1 text-sm " +
                  ((view ?? defaultView) === kind
                    ? "border-brand text-brand font-semibold"
                    : "border-line text-muted")
                }
              >
                {label} {n}
              </button>
            ))}
          </div>
        )}
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
              render: (p) => {
                if (p.published) return t("yes");
                // Without the cabinet we know nothing and say nothing: a row
                // guessing "no card" would send the owner to create one that
                // may already exist.
                if (!cabinet)
                  return <span className="text-muted">{t("no")}</span>;
                return ready.has(p.product_id) ? (
                  <span className="text-green-700">{t("stateReady")}</span>
                ) : (
                  <span className="text-muted">{t("stateNoCard")}</span>
                );
              },
            },
          ]}
          rows={candidates?.products ?? []}
          rowId={(p) => p.product_id}
          total={candidates?.total ?? 0}
          page={candPage}
          pageSize={candidates?.page_size ?? 100}
          onPage={setCandPage}
          selectable
          // Publishing pulls the cabinet's card list and matches SKUs, so it
          // works off an explicit list; "everything by filter" here would mean
          // a non-stop sweep over the catalogue on the marketplace side.
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
        {/* "Связать по артикулу" lived here and did what "Опубликовать" does,
            only over the whole catalogue: both fetch the cabinet's articles and
            write the pairs that match. Worse, its own counters called the two
            states one thing — "без связи: 105" for 104 products with no card
            and one that only needed a click — which is the conflation the state
            buttons above exist to undo. */}
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
      </section>

      {/* Cards in the cabinet with no product of ours. This used to appear only
          after pressing a button; the cabinet call the tab already makes on open
          answers it for free, so the owner sees it without asking. */}
      {!!cabinet?.orphan_skus?.length && (
        <section className="card flex flex-col gap-2">
          <div>
            <h2 className="font-bold">{t("extraOnOzon")}</h2>
            <p className="hint">{t("extraOnOzonHint")}</p>
          </div>
          <ul className="flex flex-wrap gap-2">
            {cabinet.orphan_skus.map((o) => (
              <li
                key={o}
                className="border-line rounded border px-2 py-1 text-sm"
              >
                {o}
              </li>
            ))}
          </ul>
          {cabinet.orphans > cabinet.orphan_skus.length && (
            <p className="hint">
              {t("extraOnOzonMore", {
                n: cabinet.orphans - cabinet.orphan_skus.length,
              })}
            </p>
          )}
        </section>
      )}

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
