import { useEffect, useRef, useState } from "react";
import { api, apiError, type ImportDiff } from "./api";
import { toRubles } from "./money";
import { useT, type Phrase } from "./i18n";
import { useJob } from "./useJob";
import JobBar from "./JobBar";
import { signOf } from "./shop";

type Src = "ozon" | "wb" | "yml" | "csv";

const kGuide: Record<Src, Phrase> = {
  ozon: {
    ru: "Кабинет продавца Ozon → Настройки → API-ключи → «Сгенерировать ключ». Скопируйте Client-Id и Api-Key.",
    en: "Ozon seller account → Settings → API keys → “Generate key”. Copy the Client-Id and the Api-Key.",
  },
  wb: {
    ru: "Кабинет WB → Настройки → Доступ к API → «Создать токен», отметьте категории «Контент» и «Цены и скидки». Скопируйте токен.",
    en: "Wildberries account → Settings → API access → “Create token”, tick the “Content” and “Prices and discounts” scopes. Copy the token.",
  },
  csv: {
    ru: "Свой файл по нашему шаблону: скачайте, заполните в Excel и сохраните как CSV. Русский Excel сохраняет в кодировке Windows и с точкой с запятой — это нормально, мы поймём.",
    en: "Your own file from our template: download it, fill it in a spreadsheet and save as CSV. Russian Excel saves in a legacy encoding with semicolons — that is fine, we handle it.",
  },
  yml: {
    ru: "Стандартная выгрузка Битрикс/InSales/Тильды для Яндекс.Маркета — ссылка на XML. Ключи и доступы не нужны.",
    en: "The standard Битрикс/InSales/Тильда feed for Яндекс.Маркет — just the XML link. No keys or access needed.",
  },
};

const kLabel: Record<Src, Phrase> = {
  ozon: { ru: "Ozon", en: "Ozon" },
  wb: { ru: "Wildberries", en: "Wildberries" },
  yml: { ru: "Ссылка на выгрузку (YML)", en: "Feed link (YML)" },
  csv: { ru: "Файл CSV", en: "CSV file" },
};

const kText = {
  coefficient: { ru: "Коэффициент цены", en: "Price coefficient" },
  coefficientHint: {
    ru: "Цена витрины = цена источника × коэффициент. Сюда складывают курс валюты и ввозные расходы. 1 — брать цену как есть.",
    en: "Shelf price = source price × coefficient. Fold the exchange rate and import costs into it. 1 keeps the source price as is.",
  },
  coefficientExample: {
    ru: "Пример: {source} у поставщика → {shelf} на витрине.",
    en: "Example: {source} at the supplier → {shelf} on the storefront.",
  },
  coefficientRecipe: {
    ru: "Коэффициент = курс {from}→{to} × ваша наценка. Курс мы не подставляем — возьмите его в банке: 0.0295 × 1.3 (наценка 30%) = 0.03835.",
    en: "Coefficient = the {from}→{to} rate × your markup. We do not fetch rates — take one from your bank: 0.0295 × 1.3 (a 30% markup) = 0.03835.",
  },
  rateWarning: {
    ru: "Фид отдаёт цены в {from}, а магазин продаёт в {to}. Курс мы не знаем и наружу за ним не ходим — заложите его в коэффициент, иначе цены {from} встанут на витрину со знаком {to}.",
    en: "The feed quotes prices in {from} while the shop sells in {to}. We hold no exchange rate and fetch none — fold one into the coefficient, or {from} prices land on the storefront labelled {to}.",
  },
  recompute: { ru: "Пересчитать цены", en: "Recompute prices" },
  recomputeSection: { ru: "Пересчёт цен", en: "Price recompute" },
  recomputeHint: {
    ru: "Курс изменился — поставьте новый коэффициент и пересчитайте. Считаем от цены источника, поэтому повторный пересчёт не накапливает ошибку. Цены, которые вы правили руками, не трогаем.",
    en: "When the rate moves, set a new coefficient and recompute. It is derived from the source price, so repeating it accumulates no error. Prices you edited by hand are left alone.",
  },
  recomputed: {
    ru: "Пересчитано товаров: {n}",
    en: "Products recomputed: {n}",
  },
  title: {
    ru: "Перенести товары с маркетплейса",
    en: "Move products from a marketplace",
  },
  subtitle: {
    ru: "Карточки с фото, ценами и описаниями переедут в ваш магазин. Ключи нужны только на время переноса — мы их не сохраняем.",
    en: "Listings with their photos, prices and descriptions move into your shop. The keys are used only for the transfer — we never store them.",
  },
  urlLabel: { ru: "Ссылка на выгрузку", en: "Feed link" },
  stockLabel: { ru: "Остаток по умолчанию", en: "Default stock" },
  stockHint: {
    ru: "YML не передаёт количество — только «в наличии». Укажите, сколько единиц записать каждому товару. Ноль означает «нет в наличии»: такой товар не купят. Потом можно проставить остаток всем сразу в Товарах.",
    en: "A YML feed says only whether an item is in stock, never how many. Set the quantity to write for every product. Zero means out of stock and nobody can buy it. You can set the stock for all of them at once later, in Products.",
  },
  tokenLabel: { ru: "Токен API", en: "API token" },
  check: { ru: "Проверить", en: "Check" },
  run: { ru: "Импортировать", en: "Import" },
  downloadTemplate: { ru: "Скачать шаблон", en: "Download template" },
  refresh: { ru: "Обновить из фида", en: "Refresh from the feed" },
  refreshHint: {
    ru: "Последняя выгрузка: {url} — группа «{supplier}». Кнопка подставит ссылку и группу, останется нажать «Проверить».",
    en: 'Last feed: {url} — group "{supplier}". The button fills in the link and the group; then press Check.',
  },
  fileLabel: { ru: "Файл", en: "File" },
  supplier: { ru: "Поставщик", en: "Supplier" },
  supplierHint: {
    ru: "Чьи это товары — назовите поставщика: «Ромашка», «Оптбаза». Загрузка обновляет только свою группу: товары другого поставщика и заведённые вами вручную она не трогает.",
    en: 'Whose goods these are — name the supplier: "Acme", "Wholesale Co". An import only updates its own group: another supplier\'s goods and the ones you added by hand are left alone.',
  },
  supplierPlaceholder: { ru: "Ромашка", en: "Acme" },
  changes: { ru: "Что изменится", en: "What will change" },
  cNew: { ru: "новых: {n}", en: "new: {n}" },
  cGone: { ru: "исчезло: {n}", en: "gone: {n}" },
  cUp: { ru: "подорожало: {n}", en: "dearer: {n}" },
  cDown: { ru: "подешевело: {n}", en: "cheaper: {n}" },
  cSame: { ru: "без изменений: {n}", en: "unchanged: {n}" },
  cConflicts: {
    ru: "чужих артикулов: {n}",
    en: "articles of another group: {n}",
  },
  cNoSKU: { ru: "без артикула: {n}", en: "without an article: {n}" },
  goneHint: {
    ru: "Исчезнувшим из выгрузки остаток будет обнулён — товар и его адрес останутся, на Ozon уедет ноль.",
    en: "Anything gone from the feed has its stock zeroed — the product and its URL stay, and Ozon gets a zero.",
  },
  conflictHint: {
    ru: "Эти артикулы уже принадлежат другой группе. Импорт их не тронет — решите, чьи они.",
    en: "These articles already belong to another group. The import leaves them alone — decide whose they are.",
  },
  listNew: { ru: "Новинки", en: "New" },
  listGone: { ru: "Исчезли из выгрузки", en: "Gone from the feed" },
  listPrices: {
    ru: "Сильнее всего изменилась цена",
    en: "Biggest price moves",
  },
  shownOf: {
    ru: "показано {shown} из {total}",
    en: "showing {shown} of {total}",
  },
  found: {
    ru: "Нашли товаров: {total}. Можно импортировать.",
    en: "Products found: {total}. Ready to import.",
  },
  running: {
    ru: "Импортируем. Страницу можно закрыть — импорт идёт на сервере.",
    en: "Importing. You can close the page — it runs on the server.",
  },
  stopped: { ru: " Остановлено вами.", en: " Stopped by you." },
  done: {
    ru: "Готово. Создано: {imported}, обновлено: {updated}, обнулено: {zeroed}, без изменений: {skipped}",
    en: "Done. Created: {imported}, updated: {updated}, zeroed: {zeroed}, unchanged: {skipped}",
  },
  doneErrors: { ru: ". Ошибок: {errors}", en: ". Errors: {errors}" },
  errorPrefix: { ru: "Ошибка", en: "Error" },
  error: { ru: "Ошибка: {reason}", en: "Error: {reason}" },
  errorReason: { ru: "проверьте ключи", en: "check the keys" },
};

export default function Import() {
  const [src, setSrc] = useState<Src>("ozon");
  const [clientId, setClientId] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [token, setToken] = useState("");
  const [url, setUrl] = useState("");
  // One, not zero: YML only says "in stock", and a default stock of zero would
  // make the whole imported catalogue unbuyable.
  const [stock, setStock] = useState("1");
  const [coefficient, setCoefficient] = useState("1");
  const [supplier, setSupplier] = useState("");
  const [fileB64, setFileB64] = useState("");
  const [feed, setFeed] = useState<{ url: string; supplier: string } | null>(
    null,
  );
  const [suppliers, setSuppliers] = useState<string[]>([]);
  const [diff, setDiff] = useState<ImportDiff | null>(null);
  const [currency, setCurrency] = useState("RUB");
  const [recomputeMsg, setRecomputeMsg] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const coef = Number(coefficient.replace(",", ".")) || 1;
  // Before "Check" we only know the feed currency for marketplaces: their
  // cabinet is always in rubles. The owner fills their own CSV in the shop's
  // money.
  const feedMoney = diff?.currency || (src === "csv" ? currency : "RUB");
  const t = useT(kText);
  const guide = useT(kGuide);
  const label = useT(kLabel);

  // The import lives on the server: the result comes from the job, not from the
  // launch response, so a reloaded page loses nothing.
  const job = useJob(() => {
    if (jobDone.current) setMsg(jobDone.current);
    api.importSuppliers().then((x) => setSuppliers(x.suppliers));
  });
  const jobDone = useRef("");
  if (job && !job.running && job.kind === "import") {
    const r = job.result;
    jobDone.current = job.error
      ? t("error", { reason: job.error })
      : r
        ? t("done", {
            imported: r.imported,
            updated: r.updated,
            zeroed: r.zeroed,
            skipped: r.skipped,
          }) +
          (r.errors ? t("doneErrors", { errors: r.errors }) : "") +
          (job.stopped ? t("stopped") : "")
        : "";
  }

  useEffect(() => {
    api.settings().then((s) => setCurrency(s.currency));
    api.importSuppliers().then((r) => setSuppliers(r.suppliers));
    api.importFeed().then((f) => {
      if (f.url) setFeed(f);
    });
  }, []);

  const body = (): Record<string, unknown> => {
    const common = {
      source: src,
      supplier: supplier.trim(),
      coefficient: Number(coefficient.replace(",", ".")) || 1,
    };
    if (src === "ozon")
      return { ...common, client_id: clientId, api_key: apiKey };
    if (src === "wb") return { ...common, token };
    if (src === "csv") return { ...common, file_base64: fileB64 };
    return { ...common, url, default_stock: Number(stock) || 0 };
  };

  const recompute = async () => {
    setBusy(true);
    setRecomputeMsg("");
    try {
      const r = await api.recomputePrices(
        Number(coefficient.replace(",", ".")) || 1,
      );
      setRecomputeMsg(t("recomputed", { n: r.updated }));
    } catch {
      setRecomputeMsg(t("error", { reason: t("errorReason") }));
    } finally {
      setBusy(false);
    }
  };

  const fail = (e: unknown) =>
    setMsg(t("error", { reason: apiError(e) ?? t("errorReason") }));

  return (
    <div className="form-page">
      <div>
        <h1 className="text-xl font-bold">{t("title")}</h1>
        <p className="hint mt-1">{t("subtitle")}</p>
      </div>

      <JobBar job={job} />

      <section className="card flex flex-col gap-4">
        <div className="flex gap-2">
          {(["ozon", "wb", "yml", "csv"] as Src[]).map((s) => (
            <button
              key={s}
              onClick={() => {
                setSrc(s);
                setMsg("");
              }}
              className={
                "cursor-pointer rounded-lg border px-4 py-2 font-semibold transition-colors " +
                (src === s
                  ? "border-brand bg-brand-soft text-brand"
                  : "border-line text-muted hover:text-ink bg-white")
              }
            >
              {label(s)}
            </button>
          ))}
        </div>

        <p className="hint">{guide(src)}</p>

        {feed && (
          <div className="border-line rounded-lg border p-3">
            <button
              className="btn-ghost"
              onClick={() => {
                setSrc("yml");
                setUrl(feed.url);
                setSupplier(feed.supplier);
              }}
            >
              {t("refresh")}
            </button>
            <p className="hint mt-2">
              {t("refreshHint", { url: feed.url, supplier: feed.supplier })}
            </p>
          </div>
        )}

        {src === "csv" ? (
          <div>
            <div className="mb-2">
              <a className="btn-ghost inline-block" href="/api/import/template">
                {t("downloadTemplate")}
              </a>
            </div>
            <label className="label">{t("fileLabel")}</label>
            <input
              type="file"
              accept=".csv,text/csv"
              className="text-sm"
              onChange={async (e) => {
                const f = e.target.files?.[0];
                if (!f) return;
                // Read as bytes, not text: the server detects the encoding,
                // otherwise cp1251 turns into garbage already in the browser.
                const buf = new Uint8Array(await f.arrayBuffer());
                let bin = "";
                for (const b of buf) bin += String.fromCharCode(b);
                setFileB64(btoa(bin));
              }}
            />
          </div>
        ) : src === "yml" ? (
          <>
            <div>
              <label className="label">{t("urlLabel")}</label>
              <input
                className="field"
                name="feed-url"
                autoComplete="off"
                placeholder="https://example.ru/catalog_export.xml"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
              />
            </div>
            <div>
              <label className="label">{t("stockLabel")}</label>
              <input
                className="field"
                type="number"
                min="0"
                value={stock}
                onChange={(e) => setStock(e.target.value)}
              />
              <p className="hint mt-1">{t("stockHint")}</p>
            </div>
          </>
        ) : src === "ozon" ? (
          <>
            <div>
              <label className="label">Client-Id</label>
              <input
                className="field"
                name="import-client-id"
                autoComplete="off"
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
              />
            </div>
            <div>
              <label className="label">Api-Key</label>
              <input
                className="field"
                name="import-api-key"
                autoComplete="new-password"
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
              />
            </div>
          </>
        ) : (
          <div>
            <label className="label">{t("tokenLabel")}</label>
            <input
              className="field"
              value={token}
              name="import-token"
              autoComplete="new-password"
              type="password"
              onChange={(e) => setToken(e.target.value)}
            />
          </div>
        )}

        <div>
          <label className="label">{t("supplier")}</label>
          <input
            className="field w-64"
            list="supplier-groups"
            placeholder={t("supplierPlaceholder")}
            value={supplier}
            onChange={(e) => setSupplier(e.target.value)}
          />
          <datalist id="supplier-groups">
            {suppliers.map((s) => (
              <option key={s} value={s} />
            ))}
          </datalist>
          <p className="hint mt-1">{t("supplierHint")}</p>
        </div>

        <div>
          <label className="label">{t("coefficient")}</label>
          <input
            className="field w-40"
            inputMode="decimal"
            value={coefficient}
            onChange={(e) => setCoefficient(e.target.value)}
          />
          <p className="hint mt-1">{t("coefficientHint")}</p>
          <p className="hint mt-1">
            {t("coefficientExample", {
              source: `1000 ${signOf(feedMoney)}`,
              shelf: `${(1000 * coef).toFixed(2)} ${signOf(currency)}`,
            })}
          </p>
          {feedMoney !== currency && (
            <p className="hint mt-1">
              {t("coefficientRecipe", { from: feedMoney, to: currency })}
            </p>
          )}
        </div>

        <div className="flex items-center gap-3">
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => {
              setBusy(true);
              setDiff(null);
              api
                .importCheck(body())
                .then((r) => {
                  setDiff(r);
                  setMsg(t("found", { total: r.total }));
                })
                .catch(fail)
                .finally(() => setBusy(false));
            }}
          >
            {t("check")}
          </button>
          <button
            className="btn"
            disabled={busy}
            onClick={() => {
              setBusy(true);
              setMsg(t("running"));
              setDiff(null);
              api
                .importRun(body())
                .catch(fail)
                .finally(() => setBusy(false));
            }}
          >
            {t("run")}
          </button>
        </div>

        {diff && (
          <div className="border-line flex flex-col gap-3 border-t pt-4">
            <div>
              <h2 className="font-bold">{t("changes")}</h2>
              <p className="hint">
                {[
                  t("cNew", { n: diff.new }),
                  t("cGone", { n: diff.gone }),
                  t("cUp", { n: diff.price_up }),
                  t("cDown", { n: diff.price_down }),
                  t("cSame", { n: diff.unchanged }),
                  ...(diff.conflicts
                    ? [t("cConflicts", { n: diff.conflicts })]
                    : []),
                  ...(diff.no_sku ? [t("cNoSKU", { n: diff.no_sku })] : []),
                ].join(" · ")}
              </p>
              {diff.currency && diff.currency !== currency && (
                <p className="mt-1 text-sm font-medium text-amber-700">
                  {t("rateWarning", { from: diff.currency, to: currency })}
                </p>
              )}
              {diff.conflicts > 0 && (
                <p className="hint mt-1 text-red-600">{t("conflictHint")}</p>
              )}
            </div>

            {diff.price_changes.length > 0 && (
              <div>
                <h3 className="font-semibold">{t("listPrices")}</h3>
                <table className="mt-1 w-full text-left text-sm">
                  <tbody>
                    {diff.price_changes.map((r) => (
                      <tr
                        key={r.sku}
                        className="border-line border-b last:border-0"
                      >
                        <td className="py-1">{r.title || r.sku}</td>
                        <td className="text-muted py-1 whitespace-nowrap">
                          {toRubles(r.was)} → {toRubles(r.now)}
                        </td>
                        <td
                          className={
                            "py-1 text-right whitespace-nowrap " +
                            (r.percent > 0 ? "text-red-600" : "text-green-700")
                          }
                        >
                          {r.percent > 0 ? "+" : ""}
                          {r.percent.toFixed(0)}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {diff.price_up + diff.price_down >
                  diff.price_changes.length && (
                  <p className="hint mt-1">
                    {t("shownOf", {
                      shown: diff.price_changes.length,
                      total: diff.price_up + diff.price_down,
                    })}
                  </p>
                )}
              </div>
            )}

            {diff.new_items.length > 0 && (
              <div>
                <h3 className="font-semibold">{t("listNew")}</h3>
                <ul className="mt-1 flex flex-col gap-1 text-sm">
                  {diff.new_items.map((r) => (
                    <li key={r.sku}>
                      {r.title || r.sku}
                      <span className="text-muted"> — {toRubles(r.shelf)}</span>
                    </li>
                  ))}
                </ul>
                {diff.new > diff.new_items.length && (
                  <p className="hint mt-1">
                    {t("shownOf", {
                      shown: diff.new_items.length,
                      total: diff.new,
                    })}
                  </p>
                )}
              </div>
            )}

            {diff.gone_items.length > 0 && (
              <div>
                <h3 className="font-semibold">{t("listGone")}</h3>
                <p className="hint">{t("goneHint")}</p>
                <ul className="mt-1 flex flex-col gap-1 text-sm">
                  {diff.gone_items.map((r) => (
                    <li key={r.sku}>
                      {r.title || r.sku}
                      <span className="text-muted"> — {r.stock}</span>
                    </li>
                  ))}
                </ul>
                {diff.gone > diff.gone_items.length && (
                  <p className="hint mt-1">
                    {t("shownOf", {
                      shown: diff.gone_items.length,
                      total: diff.gone,
                    })}
                  </p>
                )}
              </div>
            )}
          </div>
        )}

        {msg && (
          <p
            className={
              msg.startsWith(t("errorPrefix"))
                ? "text-red-600"
                : "text-green-700"
            }
          >
            {msg}
          </p>
        )}
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("recomputeSection")}</h2>
          <p className="hint">{t("recomputeHint")}</p>
        </div>
        <div className="flex items-center gap-3">
          <button className="btn-ghost" disabled={busy} onClick={recompute}>
            {t("recompute")}
          </button>
          {recomputeMsg && (
            <span
              className={
                recomputeMsg.startsWith(t("errorPrefix"))
                  ? "text-red-600"
                  : "text-green-700"
              }
            >
              {recomputeMsg}
            </span>
          )}
        </div>
      </section>
    </div>
  );
}
