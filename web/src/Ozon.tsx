import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  api,
  type OzonSettings,
  type OzonLinkPage,
  type Warehouse,
  type OzonOrderPage,
  type PriceRule,
  type Candidate,
  type CabinetState,
  type OzonLink,
  type OzonOrder,
  type CandidateView,
  type UnlinkedProduct,
} from "./api";
import { useLang, useT } from "./i18n";
import { toRubles, toMinor } from "./money";
import { useSign } from "./shop";
import { useFeedback } from "./feedback";
import { ChannelTabs, WarehousePicker, type ChannelTab } from "./Channel";
import PublicationPanel from "./PublicationPanel";
import DataTable from "./DataTable";
import PriceLadder from "./PriceLadder";

// Ozon posting statuses live in the same dictionary under their raw codes. An
// unknown status is shown as is: the platform keeps adding new ones, and hiding
// them behind "-" is worse than showing the raw code.
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

  errorCheckKeys: { ru: "проверьте ключи", en: "check your keys" },

  intro: {
    ru: "Свяжите товары магазина с карточками кабинета Ozon, чтобы дальше управлять остатками и ценами из одного места.",
    en: "Link your shop products to their Ozon listings so you can manage stock and prices from one place.",
  },

  connection: { ru: "Подключение", en: "Connection" },
  fbsOnly: {
    ru: "Работаем по схеме FBS - товары лежат на вашем складе, и остатками управляет магазин. Товары на складе Ozon (FBO) синхронизировать нельзя: их остаток знает только площадка, и продать их с витрины вы не сможете.",
    en: "We work with FBS - the goods are in your warehouse and the shop manages the stock. Products stored at Ozon (FBO) cannot be synced: only the platform knows their stock, and you cannot ship them from your storefront.",
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
    ru: "Складов в кабинете может быть несколько - остатки уезжают на этот. У товара один остаток, разделить его между складами магазин не умеет.",
    en: "A cabinet may hold several warehouses - stock travels to this one. A product carries a single stock figure, and the shop cannot split it between warehouses.",
  },
  noWarehouses: {
    ru: "Складов не нашлось - впишите id вручную",
    en: "No warehouses found - enter the id manually",
  },
  // The cabinet's currency is not ours to keep: one shop is one legal entity is
  // one money. When the check disagrees with the shop, the shop is what is wrong.
  currencyMismatch: {
    ru: "Кабинет торгует в {cabinet}, а магазин - в {shop}. Цены уедут в валюте магазина и площадка их отобьёт. Поправьте валюту в Профиле.",
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
    ru: "Кабинет {name}: товаров - {n}, валюта {cur}",
    en: "Account {name}: products - {n}, currency {cur}",
  },

  publicationHint: {
    ru: "Отметьте товары, которые продаёте на Ozon. В канал уезжают только отмеченные - остальные живут на витрине и площадку не видят.",
    en: "Tick the products you sell on Ozon. Only ticked ones go to the channel; the rest live on the storefront and never reach the marketplace.",
  },
  publishedResult: { ru: "Опубликовано: {n}", en: "Published: {n}" },
  // Publishing writes the link, but stock travels only from a warehouse. An
  // owner who never picked one sees "published" and then nothing happens on the
  // platform, with no error anywhere: the worker skips stocks in silence.
  noWarehouse: {
    ru: " Остатки не поедут: не задан склад FBS. Выберите его в настройках выше, а если список пуст - заведите склад в кабинете Ozon: через API склады не создаются.",
    en: " Stock will not travel: no FBS warehouse is set. Pick one in the settings above, and if the list is empty, create a warehouse in the Ozon cabinet - the API does not create them.",
  },
  unpublishedResult: { ru: "Снято: {n}", en: "Unpublished: {n}" },

  linkedProducts: { ru: "Связанные товары", en: "Linked products" },
  linkedProductsHint: {
    ru: "Цена на Ozon - ваша цена на площадке, отдельная от витринной. Пустая цена значит, что мы её не трогаем: на Ozon останется та, что стоит в кабинете.",
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
    ru: "Заказы площадки: они списывают остаток в магазине, но в раздел «Заказы» и налоговый CSV не попадают - по ним отчитывается сам Ozon.",
    en: "Marketplace orders: they draw down shop stock, but never show up under Orders or in the tax CSV - Ozon reports them itself.",
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
  salesOff: {
    ru: "Заказы не загружаются: синхронизация выключена на вкладке «Подключение».",
    en: "Orders are not being fetched: sync is off on the Connection tab.",
  },
  noSales: { ru: "Продаж на Ozon пока не было.", en: "No Ozon sales yet." },
  colPosting: { ru: "Отправление", en: "Shipment" },
  colDate: { ru: "Дата", en: "Date" },
  colItems: { ru: "Состав", en: "Items" },
  itemUnmatched: {
    ru: "{offer} × {qty} - не смогли сопоставить товар",
    en: "{offer} × {qty} - could not match a product",
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
  const { fail, line } = useFeedback(kText.errorCheckKeys);
  const [s, setS] = useState<OzonSettings | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [warehouses, setWarehouses] = useState<Warehouse[] | null>(null);
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
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [candTotal, setCandTotal] = useState(0);
  const [candPage, setCandPage] = useState(1);
  const [candSearch, setCandSearch] = useState("");
  const [candQuery, setCandQuery] = useState("");
  // Which slice the table shows. The tab opens on the only state with an action
  // attached: a live catalogue is a wall of rows and the cabinet holds a few
  // dozen cards, so "everything" hid the handful a button could do anything
  // with.
  const [view, setView] = useState<CandidateView["kind"] | null>(null);
  const [pubMsg, setPubMsg] = useState("");
  const [noCard, setNoCard] = useState<UnlinkedProduct[]>([]);
  // Asked once when the tab opens, and again after publishing changed the
  // answer. Deliberately not part of the paged candidates call: that one runs
  // per page of a hundred rows and would re-read the cabinet every time.
  const [cabinet, setCabinet] = useState<CabinetState | null>(null);
  // A configured channel is opened to see what sold, not to retype the key.
  const [tab, setTab] = useState<ChannelTab>("tabSetup");
  const tabPicked = useRef(false);
  const [zeroFailed, setZeroFailed] = useState<UnlinkedProduct[]>([]);

  const loadLinks = useCallback(
    () => api.ozonLinks(linkPage).then(setLinks),
    [linkPage],
  );

  // Open on the pile with a button attached; fall back to what is already
  // linked, and only then to everything - a shop with no cabinet answer at all
  // still gets the table it had before this existed.
  const defaultView: CandidateView["kind"] = !cabinet
    ? "all"
    : cabinet.ready > 0
      ? "ready"
      : cabinet.linked > 0
        ? "linked"
        : "all";

  const readyIDs = useMemo(() => cabinet?.ready_ids ?? [], [cabinet]);

  const loadCandidates = useCallback(async () => {
    const page = await api.ozonCandidates(candPage, candQuery, {
      kind: view ?? defaultView,
      readyIDs,
    });
    setCandidates(page.products);
    setCandTotal(page.total);
  }, [candPage, candQuery, view, defaultView, readyIDs]);

  const loadCabinet = useCallback(
    // A shop with no keys, or a platform that will not answer, simply gets no
    // summary - the table worked before this existed and must keep working.
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
    void loadCandidates();
  }, [loadCandidates]);
  // Typing in the picker must not hit the API on every keystroke.
  useEffect(() => {
    const timer = setTimeout(() => {
      setCandPage(1);
      setCandQuery(candSearch.trim());
    }, 300);
    return () => clearTimeout(timer);
  }, [candSearch]);
  // Chosen once, when the settings first arrive: a connected channel opens on
  // its sales, a fresh one on the form that makes it work. Later renders must
  // not fight the operator's own click.
  if (s && !tabPicked.current) {
    tabPicked.current = true;
    if (s.api_key_set) setTab("tabSales");
  }

  if (!s) return null;

  // Both push kinds in one table: the owner cares about the row that did not
  // reach Ozon, not about which of the two calls carried it.
  const syncErrors = [
    ...s.stock_errors.map((e) => ({
      key: `stock-${e.product_id}`,
      offer_id: e.offer_id,
      kind: t("kindStock"),
      want: String(e.stock),
      pushed: e.pushed < 0 ? "-" : String(e.pushed),
      error: e.error,
    })),
    ...s.price_errors.map((e) => ({
      key: `price-${e.product_id}`,
      offer_id: e.offer_id,
      kind: t("kindPrice"),
      want: `${toRubles(e.price)} ${s.currency}`,
      pushed: e.pushed < 0 ? "-" : `${toRubles(e.pushed)} ${s.currency}`,
      error: e.error,
    })),
  ];

  const refresh = async () => setS(await api.ozonSettings());

  const run = async (
    setMessage: (m: string) => void,
    action: () => Promise<string>,
  ) => {
    setBusy(true);
    setMessage("");
    try {
      setMessage(await action());
      await refresh();
    } catch (e) {
      setMessage(fail(e));
    } finally {
      setBusy(false);
    }
  };

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
    await run(setPriceMsg, async () => {
      await api.ozonSetPrice(productId, minor);
      await loadLinks();
      return "";
    });
  };

  const saveLadder = () =>
    run(setLadderMsg, async () => {
      setRules((await api.ozonSetPriceRules(rules)).rules);
      return t("ladderSaved");
    });

  const applyLadder = () =>
    run(setLadderMsg, async () => {
      const r = await api.ozonFillByRules();
      await loadLinks();
      return t("filled", { n: r.filled });
    });

  const fillPrices = () =>
    run(setPriceMsg, async () => {
      const r = await api.ozonFillPrices(Math.round(Number(markup) * 100));
      await loadLinks();
      return t("filled", { n: r.filled });
    });

  const save = () =>
    run(setMsg, async () => {
      const body: Record<string, unknown> = { ...s };
      if (apiKey) body.api_key = apiKey;
      setS(await api.saveOzonSettings(body));
      setApiKey("");
      return t("saved");
    });

  const check = () =>
    run(setMsg, async () => {
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
      return checked + clash;
    });

  const afterPublishChange = () =>
    Promise.all([loadLinks(), loadCandidates(), loadCabinet()]);

  const publish = (ids: number[]) =>
    run(setPubMsg, async () => {
      setNoCard([]);
      setZeroFailed([]);
      const r = await api.ozonPublish(ids);
      setNoCard(r.no_card);
      await afterPublishChange();
      return (
        t("publishedResult", { n: r.published }) +
        (r.published > 0 && !s.warehouse_id ? t("noWarehouse") : "")
      );
    });

  const unpublish = (ids: number[]) =>
    run(setPubMsg, async () => {
      setNoCard([]);
      setZeroFailed([]);
      const r = await api.ozonUnpublish(ids);
      setZeroFailed(r.failed);
      await afterPublishChange();
      return t("unpublishedResult", { n: r.unpublished });
    });

  // If the method answers in a way we did not expect, the field stays manual
  // and the owner sees why.
  const loadWarehouses = () =>
    run(setMsg, async () => {
      const list = await api.ozonWarehouses().catch((e: unknown) => {
        setWarehouses(null);
        throw e;
      });
      setWarehouses(list);
      return list.length === 0 ? t("noWarehouses") : "";
    });

  const push = () =>
    run(setStockMsg, async () => {
      const r = await api.ozonPush();
      setOrders(await api.ozonOrders(page));
      await loadLinks();
      return t("pushed", { pushed: r.pushed, failed: r.failed });
    });

  return (
    <div className="page flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-bold">Ozon</h1>
        <p className="hint mt-1">{t("intro")}</p>
      </div>

      <ChannelTabs active={tab} onSelect={setTab} />

      {tab === "tabSetup" && (
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
          <WarehousePicker
            name="ozon-warehouse"
            label={t("warehouse")}
            hint={t("warehouseHint")}
            value={s.warehouse_id}
            onChange={(id) => setS({ ...s, warehouse_id: id })}
            warehouses={warehouses}
            onLoad={loadWarehouses}
            busy={busy}
          />
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
          {line(msg)}
        </section>
      )}

      {tab === "tabPublish" && (
        <PublicationPanel
          hint={t("publicationHint")}
          cabinet={cabinet}
          view={view ?? defaultView}
          onView={(kind) => {
            setView(kind);
            setCandPage(1);
          }}
          search={candSearch}
          onSearch={setCandSearch}
          candidates={candidates}
          total={candTotal}
          page={candPage}
          pageSize={100}
          onPage={setCandPage}
          onPublish={publish}
          onUnpublish={unpublish}
          message={line(pubMsg)}
          noCard={noCard}
          zeroFailed={zeroFailed}
        />
      )}

      {tab === "tabPrices" && (
        <section className="card flex flex-col gap-4">
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
                <PriceLadder rules={rules} onChange={setRules} />
                <div className="flex flex-wrap items-center gap-3">
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
                </div>
                {line(ladderMsg)}
              </div>
              <DataTable<OzonLink>
                columns={[
                  {
                    key: "title",
                    label: t("colProduct"),
                    render: (l) =>
                      l.title || (
                        <span className="text-muted">
                          {t("productDeleted")}
                        </span>
                      ),
                  },
                  {
                    key: "offer_id",
                    label: t("colArticle"),
                    hideMobile: true,
                    render: (l) => l.offer_id,
                  },
                  {
                    key: "stock",
                    label: t("colStock"),
                    render: (l) => l.stock,
                  },
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
              {line(priceMsg)}
            </div>
          )}
        </section>
      )}

      {tab === "tabSales" && (
        <>
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

            {line(stockMsg)}

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
                    render: (e) => (
                      <span className="text-red-600">{e.error}</span>
                    ),
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
              {s.api_key_set && !s.enabled && (
                <p className="hint">{t("salesOff")}</p>
              )}
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
                  render: (o) =>
                    new Date(o.created_at).toLocaleDateString(lang),
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
        </>
      )}
    </div>
  );
}
