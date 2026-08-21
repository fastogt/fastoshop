import { useCallback, useEffect, useMemo, useState } from "react";

import {
  api,
  apiError,
  type PriceRule,
  type WBCandidate,
  type CabinetState,
  type WBLink,
  type WBOrder,
  type WBSettings,
  type WBUnlinkedProduct,
  type WBWarehouse,
} from "./api";
import DataTable from "./DataTable";
import { IconDownload, IconUpload } from "./Icons";
import { useLang, useT } from "./i18n";
import { toMinor, toRubles } from "./money";

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
  loadWarehouses: { ru: "Загрузить склады", en: "Load warehouses" },
  warehouseHint: {
    ru: "Склад нужен только для остатков. Без него цены всё равно отправляются.",
    en: "The warehouse is only needed for stock. Prices are sent without it.",
  },
  enabled: { ru: "Синхронизировать", en: "Sync enabled" },
  save: { ru: "Сохранить", en: "Save" },
  check: { ru: "Проверить", en: "Check" },
  checkOk: {
    ru: "Кабинет отвечает. Карточек: {n}",
    en: "The account answers. Cards: {n}",
  },
  checkWho: { ru: "Продавец: {name}", en: "Seller: {name}" },
  errorPrefix: { ru: "Ошибка", en: "Error" },
  errorCheckToken: { ru: "проверьте токен", en: "check the token" },

  publication: { ru: "Публикация", en: "Publication" },
  searchPlaceholder: {
    ru: "Поиск по названию или артикулу",
    en: "Search by title or article",
  },
  publish: { ru: "Опубликовать", en: "Publish" },
  unpublish: { ru: "Снять", en: "Unpublish" },
  published: { ru: "На площадке", en: "On the platform" },
  yes: { ru: "да", en: "yes" },
  cabinetSummary: {
    ru: "В кабинете карточек: {cards}. Связано: {linked}, можно связать: {ready}, карточки нет: {noCard}.",
    en: "Cards in the cabinet: {cards}. Linked: {linked}, ready to link: {ready}, no card: {noCard}.",
  },
  cabinetAmbiguous: {
    ru: "Ещё {n} товаров нашли карточку с несколькими размерами — её нельзя связать по одному артикулу.",
    en: "Another {n} products matched a card with several sizes, which one article cannot link.",
  },
  cabinetOrphans: {
    ru: "Ещё {n} карточек на площадке не совпали ни с одним товаром.",
    en: "Another {n} cards on the platform matched no product of yours.",
  },
  stateReady: { ru: "можно связать", en: "ready to link" },
  stateNoCard: { ru: "нет карточки", en: "no card" },
  no: { ru: "нет", en: "no" },
  publishDone: { ru: "Связано карточек: {n}", en: "Cards linked: {n}" },
  unpublishDone: {
    ru: "Снято с площадки: {n}",
    en: "Taken off the platform: {n}",
  },
  noCard: { ru: "Карточка не найдена:", en: "No card found:" },
  zeroFailed: {
    ru: "Не удалось обнулить остаток, связь оставлена:",
    en: "The stock could not be zeroed, the link was kept:",
  },

  linking: { ru: "Связь с карточками", en: "Card linking" },
  linkByArticle: { ru: "Связать по артикулу", en: "Link by article" },
  linkHint: {
    ru: "Артикул товара сверяется с артикулом продавца в карточке. Штрихкод берётся из самой карточки — в прайсе его нет и быть не должно.",
    en: "The product article is matched against the seller's article on the card. The barcode is read off the card itself — a price list does not carry one.",
  },
  linkDone: { ru: "Связано: {n}", en: "Linked: {n}" },
  linkedCount: {
    ru: "Связано товаров: {n}, без карточки: {m}",
    en: "Linked: {n}, without a card: {m}",
  },
  unlinkedProducts: {
    ru: "Товары без карточки:",
    en: "Products without a card:",
  },
  unlinkedCards: { ru: "Карточки без товара:", en: "Cards without a product:" },

  ladder: { ru: "Лестница наценки", en: "Markup ladder" },
  ladderHint: {
    ru: "До какой цены какой множитель. Последняя строка — «и выше», она обязательна.",
    en: 'Which multiplier up to which price. The last row is "and above" and is required.',
  },
  upTo: { ru: "до", en: "up to" },
  andAbove: { ru: "и выше", en: "and above" },
  multiplier: { ru: "множитель", en: "multiplier" },
  addBand: { ru: "Добавить строку", en: "Add a row" },
  removeBand: { ru: "Удалить", en: "Remove" },
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
    ru: "Остатки — ждут отправки: {n}, с ошибкой: {m}",
    en: "Stock — waiting: {n}, failed: {m}",
  },
  priceCounters: {
    ru: "Цены — ждут отправки: {n}, в пути: {f}, с ошибкой: {m}",
    en: "Prices — waiting: {n}, in flight: {f}, failed: {m}",
  },
  inFlightHint: {
    ru: "Wildberries принимает цены задачей и отвечает о результате позже, поэтому «в пути» — это нормальное состояние, а не ошибка.",
    en: 'Wildberries accepts prices as a task and reports the result later, so "in flight" is a normal state, not a failure.',
  },
  ordersCounters: {
    ru: "Продажи — всего: {n}, продано сверх остатка: {o}, без товара: {u}",
    en: "Sales — total: {n}, oversold: {o}, unmatched: {u}",
  },

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
  hidden: { ru: "скрыт", en: "hidden" },
  emptyLinks: { ru: "Пока ничего не связано", en: "Nothing linked yet" },
  emptyOrders: { ru: "Продаж пока нет", en: "No sales yet" },
  emptyProducts: { ru: "Товаров нет", en: "No products" },
};

export default function WB() {
  const t = useT(kText);
  const lang = useLang();

  const [s, setS] = useState<WBSettings | null>(null);
  const [token, setToken] = useState("");
  const [warehouses, setWarehouses] = useState<WBWarehouse[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [linkMsg, setLinkMsg] = useState("");
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

  const [candidates, setCandidates] = useState<WBCandidate[]>([]);
  const [candPage, setCandPage] = useState(1);
  const [candTotal, setCandTotal] = useState(0);
  const [candSearch, setCandSearch] = useState("");
  const [candQuery, setCandQuery] = useState("");

  const [rules, setRules] = useState<PriceRule[]>([]);
  const [markup, setMarkup] = useState("");
  const [noCard, setNoCard] = useState<WBUnlinkedProduct[]>([]);
  const [zeroFailed, setZeroFailed] = useState<WBUnlinkedProduct[]>([]);
  const [unlinked, setUnlinked] = useState<WBUnlinkedProduct[]>([]);
  // Asked once when the tab opens, and again after publishing changed the
  // answer. Deliberately not part of the paged candidates call: that one runs
  // per page of a hundred rows and would re-read the cabinet every time.
  const [cabinet, setCabinet] = useState<CabinetState | null>(null);
  const [unlinkedCards, setUnlinkedCards] = useState<
    { nm_id: number; vendor_code: string }[]
  >([]);

  const ready = useMemo(() => new Set(cabinet?.ready_ids ?? []), [cabinet]);

  const loadCabinet = useCallback(
    // A shop with no token, or a platform that will not answer, simply gets no
    // summary — the table worked before this existed and must keep working.
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

  const loadCandidates = useCallback(async () => {
    const page = await api.wbCandidates(candPage, candQuery);
    setCandidates(page.products);
    setCandTotal(page.total);
  }, [candPage, candQuery]);

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

  if (!s) return null;

  const fail = (e: unknown) =>
    `${t("errorPrefix")}: ${apiError(e) ?? t("errorCheckToken")}`;
  const isError = (m: string) => m.startsWith(t("errorPrefix"));
  const line = (m: string) =>
    m ? (
      <p className={isError(m) ? "text-red-600" : "text-green-700"}>{m}</p>
    ) : null;

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
        (who ? `. ${t("checkWho", { name: who })}` : "")
      );
    });

  const link = () =>
    run(setLinkMsg, async () => {
      const r = await api.wbLink();
      setUnlinked(r.unlinked_products);
      setUnlinkedCards(r.unlinked_cards);
      await loadLinks();
      return t("linkDone", { n: r.linked });
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
      return t("publishDone", { n: r.published });
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

  return (
    <div className="page flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-bold">{t("heading")}</h1>
        <p className="hint mt-1">{t("lead")}</p>
      </div>

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

        <div>
          <label className="label" htmlFor="wb-warehouse">
            {t("warehouse")}
          </label>
          <div className="flex items-center gap-2">
            {warehouses ? (
              <select
                id="wb-warehouse"
                className="field"
                value={s.warehouse_id}
                onChange={(e) => setS({ ...s, warehouse_id: e.target.value })}
              >
                <option value="">—</option>
                {warehouses.map((wh) => (
                  <option key={wh.id} value={wh.id}>
                    {wh.name}
                  </option>
                ))}
              </select>
            ) : (
              <input
                id="wb-warehouse"
                className="field"
                name="wb-warehouse"
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
          <p className="hint mt-1">{t("warehouseHint")}</p>
        </div>

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

      <section className="card flex flex-col gap-4">
        <h2 className="font-bold">{t("publication")}</h2>
        {cabinet && (
          <p className="hint">
            {t("cabinetSummary", {
              cards: cabinet.cards,
              linked: cabinet.linked,
              ready: cabinet.ready,
              noCard: cabinet.no_card,
            })}
            {!!cabinet.ambiguous &&
              " " + t("cabinetAmbiguous", { n: cabinet.ambiguous })}
            {cabinet.orphans > 0 &&
              " " + t("cabinetOrphans", { n: cabinet.orphans })}
          </p>
        )}
        <input
          className="field w-64"
          placeholder={t("searchPlaceholder")}
          value={candSearch}
          onChange={(e) => setCandSearch(e.target.value)}
        />
        <DataTable<WBCandidate>
          columns={[
            {
              key: "title",
              label: t("thProduct"),
              render: (p) => (
                <span>
                  {p.title}
                  {p.hidden && (
                    <span className="text-muted ml-2 text-xs">
                      {t("hidden")}
                    </span>
                  )}
                </span>
              ),
            },
            { key: "sku", label: t("thArticle"), render: (p) => p.sku },
            { key: "stock", label: t("thStock"), render: (p) => p.stock },
            {
              key: "published",
              label: t("published"),
              render: (p) => {
                if (p.published) return t("yes");
                // Without the cabinet we know nothing and say nothing: a row
                // guessing "no card" would send the owner to create one that
                // may already exist.
                if (!cabinet) return t("no");
                return ready.has(p.product_id) ? (
                  <span className="text-green-700">{t("stateReady")}</span>
                ) : (
                  <span className="text-muted">{t("stateNoCard")}</span>
                );
              },
            },
          ]}
          rows={candidates}
          rowId={(p) => p.product_id}
          total={candTotal}
          page={candPage}
          pageSize={100}
          onPage={setCandPage}
          selectable
          // "All matching" is off: which goods go to a marketplace is a decision,
          // and a filter is not one.
          allowAll={false}
          bulkActions={[
            {
              label: t("publish"),
              icon: <IconUpload />,
              idsOnly: true,
              onClick: (sel) => void publish(sel.ids),
            },
            {
              label: t("unpublish"),
              icon: <IconDownload />,
              idsOnly: true,
              onClick: (sel) => void unpublish(sel.ids),
            },
          ]}
          emptyTitle={t("emptyProducts")}
        />
        {line(pubMsg)}
        {noCard.length > 0 && (
          <div>
            <p className="hint">{t("noCard")}</p>
            <ul className="hint list-disc pl-5">
              {noCard.map((p) => (
                <li key={p.id}>
                  {p.sku} — {p.title}
                  {p.reason && ` (${p.reason})`}
                </li>
              ))}
            </ul>
          </div>
        )}
        {zeroFailed.length > 0 && (
          <div>
            <p className="hint">{t("zeroFailed")}</p>
            <ul className="hint list-disc pl-5">
              {zeroFailed.map((p) => (
                <li key={p.id}>
                  {p.sku} — {p.title}
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>

      <section className="card flex flex-col gap-4">
        <h2 className="font-bold">{t("linking")}</h2>
        <p className="hint">
          {t("linkedCount", { n: s.linked, m: s.unlinked })}
        </p>
        <div>
          <button className="btn-ghost" disabled={busy} onClick={link}>
            {t("linkByArticle")}
          </button>
          <p className="hint mt-1">{t("linkHint")}</p>
        </div>
        {line(linkMsg)}
        {unlinked.length > 0 && (
          <div>
            <p className="hint">{t("unlinkedProducts")}</p>
            <ul className="hint list-disc pl-5">
              {unlinked.slice(0, 50).map((p) => (
                <li key={p.id}>
                  {p.sku} — {p.title}
                  {p.reason && ` (${p.reason})`}
                </li>
              ))}
            </ul>
          </div>
        )}
        {unlinkedCards.length > 0 && (
          <div>
            <p className="hint">{t("unlinkedCards")}</p>
            <ul className="hint list-disc pl-5">
              {unlinkedCards.slice(0, 50).map((c) => (
                <li key={c.nm_id}>
                  {c.vendor_code} (nmID {c.nm_id})
                </li>
              ))}
            </ul>
          </div>
        )}

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
          <div className="mt-2 flex flex-col gap-2">
            {rules.map((r, i) => (
              <div key={i} className="flex items-center gap-2">
                {r.up_to === 0 ? (
                  <span className="text-muted w-40 text-sm">
                    {t("andAbove")}
                  </span>
                ) : (
                  <>
                    <span className="text-muted text-sm">{t("upTo")}</span>
                    <input
                      className="field w-28"
                      value={toRubles(r.up_to)}
                      onChange={(e) => {
                        const next = [...rules];
                        next[i] = { ...r, up_to: toMinor(e.target.value) };
                        setRules(next);
                      }}
                    />
                  </>
                )}
                <span className="text-muted text-sm">{t("multiplier")}</span>
                <input
                  className="field w-24"
                  value={String(r.multiplier)}
                  onChange={(e) => {
                    const next = [...rules];
                    next[i] = {
                      ...r,
                      multiplier: Number(e.target.value.replace(",", ".")),
                    };
                    setRules(next);
                  }}
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
          <div className="mt-2 flex flex-wrap gap-2">
            <button
              className="btn-ghost"
              onClick={() => {
                // The open-ended row stays last, whatever order the owner typed.
                const bands = rules.filter((r) => r.up_to !== 0);
                const open = rules.find((r) => r.up_to === 0) ?? {
                  up_to: 0,
                  multiplier: 2,
                };
                setRules([...bands, { up_to: 100000, multiplier: 2 }, open]);
              }}
            >
              {t("addBand")}
            </button>
            <button className="btn-ghost" disabled={busy} onClick={saveLadder}>
              {t("saveLadder")}
            </button>
            <button className="btn-ghost" disabled={busy} onClick={applyLadder}>
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
        {(s.stock_errors.length > 0 || s.price_errors.length > 0) && (
          <ul className="hint list-disc pl-5">
            {s.stock_errors.map((e) => (
              <li key={`s${e.product_id}`}>
                {e.barcode}: {e.error} {when(e.retry_at)}
              </li>
            ))}
            {s.price_errors.map((e) => (
              <li key={`p${e.product_id}`}>
                nmID {e.nm_id}: {e.error} {when(e.retry_at)}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card flex flex-col gap-4">
        <h2 className="font-bold">{t("sales")}</h2>
        <p className="hint">{t("salesHint")}</p>
        <DataTable<WBOrder>
          columns={[
            {
              key: "created_at",
              label: t("thDate"),
              render: (o) => new Date(o.created_at).toLocaleDateString(lang),
            },
            {
              key: "title",
              label: t("thProduct"),
              render: (o) =>
                o.product_id ? (
                  o.title
                ) : (
                  <span className="text-red-600">
                    {o.article || o.barcode} — {t("unmatched")}
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
    </div>
  );
}
