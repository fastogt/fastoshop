import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  api,
  type PriceRule,
  type Candidate,
  type CabinetState,
  type WBLink,
  type WBOrder,
  type WBSettings,
  type UnlinkedProduct,
  type Warehouse,
  type CandidateView,
} from "./api";
import { ChannelTabs, WarehousePicker, type ChannelTab } from "./Channel";
import DataTable from "./DataTable";
import { useFeedback } from "./feedback";
import { useLang, useT } from "./i18n";
import { toMinor, toRubles } from "./money";
import PriceLadder from "./PriceLadder";
import PublicationPanel from "./PublicationPanel";

const kText = {
  heading: { ru: "Wildberries", en: "Wildberries" },

  lead: {
    ru: "Витрина и кабинет Wildberries обмениваются остатками и ценами. Карточки заводит продавец в кабинете, мы связываемся с ними по артикулу.",
    en: "The storefront and the Wildberries account exchange stock and prices. Cards are created by the seller in the account; we link to them by article.",
  },

  connection: { ru: "Подключение", en: "Connection" },
  token: { ru: "Токен API", en: "API token" },
  tokenHint: {
    ru: "Кабинет WB Партнёры → Настройки → Доступ к API. Нужны разделы «Контент», «Маркетплейс» и «Цены и скидки». Токен показывается один раз.",
    en: "WB Partners → Settings → API access. The Content, Marketplace and Prices sections are required. The token is shown once.",
  },
  tokenSet: { ru: "токен сохранён", en: "token saved" },
  sandbox: { ru: "Тестовый контур", en: "Sandbox" },
  sandboxHint: {
    ru: "Запросы уходят на тестовые адреса Wildberries и не касаются настоящих товаров, заказов и баланса.",
    en: "Requests go to the Wildberries test hosts and touch no real goods, orders or balance.",
  },
  warehouse: { ru: "Склад", en: "Warehouse" },
  // A token is issued per section. One without «Маркетплейс» answers every
  // stock call with 403 while cards and prices keep working, so the tab looks
  // connected and the levels quietly stay put.
  noStockScope: {
    ru: "Но в токене нет раздела «Маркетплейс»: остатки и заказы закрыты, синхронизация склада работать не будет. Выпустите токен заново в кабинете Wildberries, отметив этот раздел.",
    en: "The token has no Marketplace section: stock and orders are refused, so warehouse sync will not work. Issue a new token in the Wildberries account with that section ticked.",
  },
  // Why one field and not a choice per product: a product carries a single
  // stock figure, so the same number sent to two warehouses would be the same
  // goods promised twice.
  warehouseHint: {
    ru: "Склад нужен только для остатков - без него цены всё равно отправляются. Складов в кабинете может быть несколько, остатки уезжают на этот: у товара один остаток, разделить его между складами магазин не умеет.",
    en: "The warehouse is only needed for stock - prices are sent without it. A cabinet may hold several warehouses and stock travels to this one: a product carries a single stock figure, and the shop cannot split it between warehouses.",
  },
  enabled: { ru: "Синхронизировать", en: "Sync enabled" },
  save: { ru: "Сохранить", en: "Save" },
  check: { ru: "Проверить", en: "Check" },
  checkOk: {
    ru: "Кабинет отвечает. Карточек: {n}",
    en: "The account answers. Cards: {n}",
  },
  checkWho: { ru: "Продавец: {name}", en: "Seller: {name}" },
  errorCheckToken: { ru: "проверьте токен", en: "check the token" },

  cabinetAmbiguous: {
    ru: "Ещё {n} товаров нашли карточку с несколькими размерами - её нельзя связать по одному артикулу.",
    en: "Another {n} products matched a card with several sizes, which one article cannot link.",
  },
  publishDone: { ru: "Связано карточек: {n}", en: "Cards linked: {n}" },
  // Publishing writes the link, but stock travels only from a warehouse. An
  // owner who never picked one sees "linked" and then nothing happens on the
  // platform, with no error anywhere: the worker skips stocks in silence.
  noWarehouse: {
    ru: " Остатки не поедут: не задан склад. Выберите его в настройках выше, а если список пуст - заведите склад в кабинете Wildberries: через API склады не создаются.",
    en: " Stock will not travel: no warehouse is set. Pick one in the settings above, and if the list is empty, create a warehouse in the Wildberries cabinet - the API does not create them.",
  },
  unpublishDone: {
    ru: "Снято с площадки: {n}",
    en: "Taken off the platform: {n}",
  },

  ladder: { ru: "Лестница наценки", en: "Markup ladder" },
  ladderHint: {
    ru: "До какой цены какой множитель. Последняя строка - «и выше», она обязательна.",
    en: 'Which multiplier up to which price. The last row is "and above" and is required.',
  },
  saveLadder: { ru: "Сохранить лестницу", en: "Save the ladder" },
  applyLadder: { ru: "Заполнить по лестнице", en: "Fill by the ladder" },
  markup: { ru: "Наценка, %", en: "Markup, %" },
  fillPrices: { ru: "Заполнить цены", en: "Fill prices" },
  filled: { ru: "Заполнено цен: {n}", en: "Prices filled: {n}" },

  sync: { ru: "Синхронизация", en: "Sync" },
  pushNow: { ru: "Отправить сейчас", en: "Push now" },
  pushDone: {
    ru: "Отправлено: {n}, с ошибкой: {m}",
    en: "Pushed: {n}, failed: {m}",
  },
  stockCounters: {
    ru: "Остатки - ждут отправки: {n}, с ошибкой: {m}",
    en: "Stock - waiting: {n}, failed: {m}",
  },
  priceCounters: {
    ru: "Цены - ждут отправки: {n}, в пути: {f}, с ошибкой: {m}",
    en: "Prices - waiting: {n}, in flight: {f}, failed: {m}",
  },
  inFlightHint: {
    ru: "Wildberries принимает цены задачей и отвечает о результате позже, поэтому «в пути» - это нормальное состояние, а не ошибка.",
    en: 'Wildberries accepts prices as a task and reports the result later, so "in flight" is a normal state, not a failure.',
  },
  ordersCounters: {
    ru: "Продажи - всего: {n}, продано сверх остатка: {o}, без товара: {u}",
    en: "Sales - total: {n}, oversold: {o}, unmatched: {u}",
  },
  kindStock: { ru: "остаток", en: "stock" },
  kindPrice: { ru: "цена", en: "price" },
  thWhat: { ru: "Что", en: "What" },
  thOurs: { ru: "У нас", en: "Ours" },
  thSent: { ru: "Отправлено", en: "Sent" },

  sales: { ru: "Продажи на площадке", en: "Sales on the platform" },
  salesHint: {
    ru: "Эти продажи не попадают в заказы магазина: площадка отчитывается по ним сама, и дублировать выручку нельзя.",
    en: "These sales never land in the shop's orders: the platform reports them itself, and doubling the revenue is not an option.",
  },

  thProduct: { ru: "Товар", en: "Product" },
  thArticle: { ru: "Артикул", en: "Article" },
  thCard: { ru: "Карточка", en: "Card" },
  thBarcode: { ru: "Штрихкод", en: "Barcode" },
  thStock: { ru: "Остаток", en: "Stock" },
  thShopPrice: { ru: "Цена в магазине", en: "Shop price" },
  thPrice: { ru: "Цена на WB", en: "Price on WB" },
  thError: { ru: "Ошибка", en: "Error" },
  thStatus: { ru: "Статус", en: "Status" },
  thDate: { ru: "Дата", en: "Date" },
  thQty: { ru: "Кол-во", en: "Qty" },
  retryAt: { ru: "повтор в {time}", en: "retry at {time}" },
  inFlight: { ru: "в пути", en: "in flight" },
  oversold: { ru: "сверх остатка", en: "oversold" },
  unmatched: { ru: "нет товара", en: "no product" },
  emptyLinks: { ru: "Пока ничего не связано", en: "Nothing linked yet" },
  salesOff: {
    ru: "Заказы не загружаются: синхронизация выключена на вкладке «Подключение».",
    en: "Orders are not being fetched: sync is off on the Connection tab.",
  },
  emptyOrders: { ru: "Продаж пока нет", en: "No sales yet" },
};

export default function WB() {
  const t = useT(kText);
  const lang = useLang();
  const { fail, line } = useFeedback(kText.errorCheckToken);

  const [s, setS] = useState<WBSettings | null>(null);
  const [token, setToken] = useState("");
  const [warehouses, setWarehouses] = useState<Warehouse[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [priceMsg, setPriceMsg] = useState("");
  const [ladderMsg, setLadderMsg] = useState("");
  const [pubMsg, setPubMsg] = useState("");
  const [syncMsg, setSyncMsg] = useState("");

  const [links, setLinks] = useState<WBLink[]>([]);
  const [linkPage, setLinkPage] = useState(1);
  const [linkTotal, setLinkTotal] = useState(0);
  const [priceDraft, setPriceDraft] = useState<Record<number, string>>({});

  const [orders, setOrders] = useState<WBOrder[]>([]);
  const [orderPage, setOrderPage] = useState(1);
  const [orderTotal, setOrderTotal] = useState(0);

  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [candPage, setCandPage] = useState(1);
  const [candTotal, setCandTotal] = useState(0);
  const [candSearch, setCandSearch] = useState("");
  const [candQuery, setCandQuery] = useState("");
  // Which slice the table shows. The tab opens on the only state with an action
  // attached: a live catalogue is a wall of rows and the cabinet holds a few
  // dozen cards, so "everything" was a wall to scroll past.
  const [view, setView] = useState<CandidateView["kind"] | null>(null);

  const [rules, setRules] = useState<PriceRule[]>([]);
  const [markup, setMarkup] = useState("");
  const [noCard, setNoCard] = useState<UnlinkedProduct[]>([]);
  const [zeroFailed, setZeroFailed] = useState<UnlinkedProduct[]>([]);
  // Asked once when the tab opens, and again after publishing changed the
  // answer. Deliberately not part of the paged candidates call: that one runs
  // per page of a hundred rows and would re-read the cabinet every time.
  const [cabinet, setCabinet] = useState<CabinetState | null>(null);
  // A configured channel is opened to see what sold, not to retype the token.
  const [tab, setTab] = useState<ChannelTab>("tabSetup");
  const tabPicked = useRef(false);

  const loadCabinet = useCallback(
    // A shop with no token, or a platform that will not answer, simply gets no
    // summary - the table worked before this existed and must keep working.
    () =>
      api
        .wbCabinet()
        .then(setCabinet)
        .catch(() => setCabinet(null)),
    [],
  );

  const loadLinks = useCallback(async () => {
    const page = await api.wbLinks(linkPage);
    setLinks(page.links);
    setLinkTotal(page.total);
  }, [linkPage]);

  // Open on the pile with a button attached; fall back to what is already
  // linked, and only then to everything.
  const defaultView: CandidateView["kind"] = !cabinet
    ? "all"
    : cabinet.ready > 0
      ? "ready"
      : cabinet.linked > 0
        ? "linked"
        : "all";

  const readyIDs = useMemo(() => cabinet?.ready_ids ?? [], [cabinet]);

  const loadCandidates = useCallback(async () => {
    const page = await api.wbCandidates(candPage, candQuery, {
      kind: view ?? defaultView,
      readyIDs,
    });
    setCandidates(page.products);
    setCandTotal(page.total);
  }, [candPage, candQuery, view, defaultView, readyIDs]);

  useEffect(() => {
    void (async () => {
      setS(await api.wbSettings());
      setRules((await api.wbPriceRules()).rules);
      await loadCabinet();
    })();
  }, [loadCabinet]);

  useEffect(() => {
    void (async () => {
      const page = await api.wbOrders(orderPage);
      setOrders(page.orders);
      setOrderTotal(page.total);
    })();
  }, [orderPage]);

  useEffect(() => void loadLinks(), [loadLinks]);
  useEffect(() => void loadCandidates(), [loadCandidates]);

  // The search box drives a server query, so it waits for a pause in typing.
  useEffect(() => {
    const id = setTimeout(() => {
      setCandQuery(candSearch);
      setCandPage(1);
    }, 300);
    return () => clearTimeout(id);
  }, [candSearch]);

  // Chosen once, when the settings first arrive: a connected channel opens on
  // its sales, a fresh one on the form that makes it work. Later renders must
  // not fight the operator's own click.
  if (s && !tabPicked.current) {
    tabPicked.current = true;
    if (s.token_set) setTab("tabSales");
  }

  if (!s) return null;

  const refresh = async () => setS(await api.wbSettings());

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

  const save = () =>
    run(setMsg, async () => {
      const body: Record<string, unknown> = {
        enabled: s.enabled,
        sandbox: s.sandbox,
        warehouse_id: s.warehouse_id,
      };
      // The token is write-only: sending nothing keeps the stored one.
      if (token) body.token = token;
      setS(await api.saveWBSettings(body));
      setToken("");
      return "";
    });

  const check = () =>
    run(setMsg, async () => {
      const r = await api.wbCheck();
      const who = r.legal_name || r.trade_mark;
      return (
        t("checkOk", { n: r.total }) +
        (who ? `. ${t("checkWho", { name: who })}` : "") +
        (r.no_stock_scope ? " " + t("noStockScope") : "")
      );
    });

  const loadWarehouses = () =>
    run(setMsg, async () => {
      setWarehouses(await api.wbWarehouses());
      return "";
    });

  const push = () =>
    run(setSyncMsg, async () => {
      const r = await api.wbPush();
      await loadLinks();
      return t("pushDone", { n: r.pushed, m: r.failed });
    });

  const publish = (ids: number[]) =>
    run(setPubMsg, async () => {
      const r = await api.wbPublish(ids);
      setNoCard(r.no_card);
      await Promise.all([loadLinks(), loadCandidates(), loadCabinet()]);
      return (
        t("publishDone", { n: r.published }) +
        (r.published > 0 && !s.warehouse_id ? t("noWarehouse") : "")
      );
    });

  const unpublish = (ids: number[]) =>
    run(setPubMsg, async () => {
      const r = await api.wbUnpublish(ids);
      setZeroFailed(r.failed);
      await Promise.all([loadLinks(), loadCandidates(), loadCabinet()]);
      return t("unpublishDone", { n: r.unpublished });
    });

  const savePrice = async (l: WBLink, value: string) => {
    const minor = toMinor(value);
    if (Number.isNaN(minor) || minor < 0 || minor === l.price) return;
    await run(setPriceMsg, async () => {
      await api.wbSetPrice(l.product_id, minor);
      await loadLinks();
      return "";
    });
  };

  const fillPrices = () =>
    run(setPriceMsg, async () => {
      const bp = Math.round(Number(markup.replace(",", ".")) * 100);
      const r = await api.wbFillPrices(Number.isNaN(bp) ? 0 : bp);
      await loadLinks();
      return t("filled", { n: r.filled });
    });

  const saveLadder = () =>
    run(setLadderMsg, async () => {
      setRules((await api.wbSetPriceRules(rules)).rules);
      return "";
    });

  const applyLadder = () =>
    run(setLadderMsg, async () => {
      const r = await api.wbFillByRules();
      await loadLinks();
      return t("filled", { n: r.filled });
    });

  const when = (iso: string | null) =>
    iso ? t("retryAt", { time: new Date(iso).toLocaleTimeString(lang) }) : "";

  // Both push kinds in one table: the owner cares about the row that did not
  // reach the platform, not about which of the two calls carried it.
  const syncErrors = [
    ...s.stock_errors.map((e) => ({
      key: `stock-${e.product_id}`,
      ref: e.barcode,
      kind: t("kindStock"),
      want: String(e.stock),
      pushed: e.pushed < 0 ? "-" : String(e.pushed),
      error: `${e.error} ${when(e.retry_at)}`.trim(),
    })),
    ...s.price_errors.map((e) => ({
      key: `price-${e.product_id}`,
      ref: `nmID ${e.nm_id}`,
      kind: t("kindPrice"),
      want: toRubles(e.price),
      pushed: e.pushed < 0 ? "-" : toRubles(e.pushed),
      error: `${e.error} ${when(e.retry_at)}`.trim(),
    })),
  ];

  return (
    <div className="page flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-bold">{t("heading")}</h1>
        <p className="hint mt-1">{t("lead")}</p>
      </div>

      <ChannelTabs active={tab} onSelect={setTab} />

      {tab === "tabSetup" && (
        <section className="card flex flex-col gap-4">
          <h2 className="font-bold">{t("connection")}</h2>
          <div>
            <label className="label" htmlFor="wb-token">
              {t("token")}
            </label>
            {/* The browser reads "text + password" as a login form and offers the
              admin password; an accepted suggestion would be saved to the shop
              database in clear text. */}
            <input
              id="wb-token"
              className="field"
              type="password"
              name="wb-api-token"
              autoComplete="new-password"
              placeholder={s.token_set ? "••••••••" : ""}
              value={token}
              onChange={(e) => setToken(e.target.value)}
            />
            <p className="hint mt-1">{t("tokenHint")}</p>
            {s.token_set && !token && (
              <p className="hint text-green-700">{t("tokenSet")}</p>
            )}
          </div>

          <WarehousePicker
            name="wb-warehouse"
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
              checked={s.sandbox}
              onChange={(e) => setS({ ...s, sandbox: e.target.checked })}
            />
            {t("sandbox")}
          </label>
          <p className="hint -mt-2">{t("sandboxHint")}</p>

          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={s.enabled}
              onChange={(e) => setS({ ...s, enabled: e.target.checked })}
            />
            {t("enabled")}
          </label>

          <div className="flex gap-2">
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
          summaryExtra={
            cabinet?.ambiguous
              ? t("cabinetAmbiguous", { n: cabinet.ambiguous })
              : undefined
          }
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
          onPublish={(ids) => void publish(ids)}
          onUnpublish={(ids) => void unpublish(ids)}
          message={line(pubMsg)}
          noCard={noCard}
          zeroFailed={zeroFailed}
        />
      )}

      {tab === "tabPrices" && (
        <section className="card flex flex-col gap-4">
          <div className="flex flex-wrap items-end gap-2">
            <div>
              <label className="label" htmlFor="wb-markup">
                {t("markup")}
              </label>
              <input
                id="wb-markup"
                className="field w-28"
                value={markup}
                onChange={(e) => setMarkup(e.target.value)}
              />
            </div>
            <button className="btn-ghost" disabled={busy} onClick={fillPrices}>
              {t("fillPrices")}
            </button>
          </div>
          {line(priceMsg)}

          <div>
            <h3 className="font-medium">{t("ladder")}</h3>
            <p className="hint mt-1">{t("ladderHint")}</p>
            <div className="mt-2">
              <PriceLadder rules={rules} onChange={setRules} />
            </div>
            <div className="mt-2 flex flex-wrap gap-2">
              <button
                className="btn-ghost"
                disabled={busy}
                onClick={saveLadder}
              >
                {t("saveLadder")}
              </button>
              <button
                className="btn-ghost"
                disabled={busy}
                onClick={applyLadder}
              >
                {t("applyLadder")}
              </button>
            </div>
            {line(ladderMsg)}
          </div>

          <DataTable<WBLink>
            columns={[
              { key: "title", label: t("thProduct"), render: (l) => l.title },
              { key: "sku", label: t("thArticle"), render: (l) => l.sku },
              {
                key: "nm_id",
                label: t("thCard"),
                hideMobile: true,
                render: (l) => l.nm_id,
              },
              {
                key: "barcode",
                label: t("thBarcode"),
                hideMobile: true,
                render: (l) => l.barcode,
              },
              { key: "stock", label: t("thStock"), render: (l) => l.stock },
              {
                key: "shop_price",
                label: t("thShopPrice"),
                hideMobile: true,
                render: (l) => toRubles(l.shop_price),
              },
              {
                key: "price",
                label: t("thPrice"),
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
                    onBlur={(e) => void savePrice(l, e.target.value)}
                  />
                ),
              },
              {
                key: "state",
                label: t("thError"),
                render: (l) => (
                  <span className="text-xs">
                    {l.in_flight && (
                      <span className="text-muted mr-2">{t("inFlight")}</span>
                    )}
                    {(l.stock_error || l.price_error) && (
                      <span className="text-red-600">
                        {l.stock_error || l.price_error}
                      </span>
                    )}
                  </span>
                ),
              },
            ]}
            rows={links}
            rowId={(l) => l.product_id}
            total={linkTotal}
            page={linkPage}
            pageSize={100}
            onPage={setLinkPage}
            emptyTitle={t("emptyLinks")}
          />
        </section>
      )}

      {tab === "tabSales" && (
        <>
          <section className="card flex flex-col gap-4">
            <h2 className="font-bold">{t("sync")}</h2>
            <p className="hint">
              {t("stockCounters", { n: s.pending, m: s.failed })}
            </p>
            <p className="hint">
              {t("priceCounters", {
                n: s.price_pending,
                f: s.price_in_flight,
                m: s.price_failed,
              })}
            </p>
            <p className="hint">{t("inFlightHint")}</p>
            <p className="hint">
              {t("ordersCounters", {
                n: s.orders_total,
                o: s.orders_oversold,
                u: s.orders_unresolved,
              })}
            </p>
            {s.poll_error && <p className="text-red-600">{s.poll_error}</p>}
            <div>
              <button className="btn" disabled={busy} onClick={push}>
                {t("pushNow")}
              </button>
            </div>
            {line(syncMsg)}
            {syncErrors.length > 0 && (
              <DataTable<(typeof syncErrors)[number]>
                columns={[
                  { key: "ref", label: t("thCard"), render: (e) => e.ref },
                  { key: "kind", label: t("thWhat"), render: (e) => e.kind },
                  { key: "want", label: t("thOurs"), render: (e) => e.want },
                  {
                    key: "pushed",
                    label: t("thSent"),
                    hideMobile: true,
                    render: (e) => e.pushed,
                  },
                  {
                    key: "error",
                    label: t("thError"),
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
            <h2 className="font-bold">{t("sales")}</h2>
            <p className="hint">{t("salesHint")}</p>
            {s.token_set && !s.enabled && (
              <p className="hint">{t("salesOff")}</p>
            )}
            <DataTable<WBOrder>
              columns={[
                {
                  key: "created_at",
                  label: t("thDate"),
                  render: (o) =>
                    new Date(o.created_at).toLocaleDateString(lang),
                },
                {
                  key: "title",
                  label: t("thProduct"),
                  render: (o) =>
                    o.product_id ? (
                      o.title
                    ) : (
                      <span className="text-red-600">
                        {o.article || o.barcode} - {t("unmatched")}
                      </span>
                    ),
                },
                {
                  key: "barcode",
                  label: t("thBarcode"),
                  hideMobile: true,
                  render: (o) => o.barcode,
                },
                { key: "qty", label: t("thQty"), render: (o) => o.qty },
                {
                  key: "status",
                  label: t("thStatus"),
                  render: (o) => (
                    <span>
                      {o.status}
                      {o.oversold && (
                        <span className="ml-2 text-xs text-red-600">
                          {t("oversold")}
                        </span>
                      )}
                    </span>
                  ),
                },
              ]}
              rows={orders}
              rowId={(o) => o.order_id}
              total={orderTotal}
              page={orderPage}
              pageSize={50}
              onPage={setOrderPage}
              emptyTitle={t("emptyOrders")}
            />
          </section>
        </>
      )}
    </div>
  );
}
