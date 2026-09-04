import { useEffect, useState } from "react";
import { api, type LogInfo, type Stats as StatsData } from "./api";
import { useLang, useT } from "./i18n";

const kText = {
  server: { ru: "Сервер", en: "Server" },
  shop: { ru: "Магазин", en: "Shop" },
  hint: {
    ru: "Снимок на момент открытия страницы. Обновите её, чтобы пересчитать.",
    en: "A snapshot taken when the page opened. Reload it to recount.",
  },
  serverHint: {
    ru: "Цифры сервера общие для всей машины: если на ней несколько магазинов, нагрузку они делят.",
    en: "Server numbers cover the whole machine: if it runs several shops, they share the load.",
  },
  cpu: { ru: "CPU", en: "CPU" },
  load: { ru: "Средняя нагрузка", en: "Load average" },
  memory: { ru: "RAM", en: "RAM" },
  disk: { ru: "HDD", en: "HDD" },
  network: { ru: "Сеть", en: "Network" },
  uptime: { ru: "Аптайм", en: "Uptime" },
  os: { ru: "Система", en: "System" },
  virt: { ru: "Виртуализация", en: "Virtualisation" },
  products: { ru: "Товары", en: "Products" },
  productsVisible: { ru: "из них на витрине", en: "visible on the storefront" },
  orders: { ru: "Заказы", en: "Orders" },
  ordersNew: { ru: "из них новых", en: "new" },
  database: { ru: "База данных", en: "Database" },
  uploads: { ru: "Фото", en: "Photos" },
  files: { ru: "файлов", en: "files" },
  process: { ru: "Процесс магазина", en: "Shop process" },
  version: { ru: "Версия", en: "Version" },
  failed: {
    ru: "Не удалось получить статистику",
    en: "Could not load the stats",
  },
  free: { ru: "свободно", en: "free" },
  // Glued to the number, so no space and no plural: "3д 4ч", "2d 4h".
  unitDay: { ru: "д", en: "d" },
  unitHour: { ru: "ч", en: "h" },
  unitMinute: { ru: "мин", en: "m" },
  expert: { ru: "Экспертный режим", en: "Expert mode" },
  expertHint: {
    ru: "Журнал магазина: что делали фоновые задачи, какие фотографии не скачались, ушло ли письмо по заказу.",
    en: "The shop's log: what the background jobs did, which photos failed to download, whether an order email went out.",
  },
  logOpen: { ru: "Открыть журнал", en: "Open the log" },
  logSize: { ru: "Размер", en: "Size" },
  logModified: { ru: "Последняя запись", en: "Last entry" },
  logMissing: {
    ru: "Журнал не ведётся: в конфигурации магазина не задан путь к файлу.",
    en: "No log is kept: the shop's configuration names no file for it.",
  },
};

// KB, MB, GB stay as they are in every language: they are technical marks, not
// words. The locale's own compact notation turned a disk into "1,2 млрд Б",
// which is arithmetically true and unreadable.
const kUnits = ["B", "KB", "MB", "GB", "TB"];

const bytes = (n: number, lang: string): string => {
  let i = 0;
  while (n >= 1024 && i < kUnits.length - 1) {
    n /= 1024;
    i++;
  }
  const digits = i === 0 || n >= 100 ? 0 : 1;
  return `${new Intl.NumberFormat(lang, { maximumFractionDigits: digits }).format(n)} ${kUnits[i]}`;
};

function duration(sec: number, u: { d: string; h: string; m: string }): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}${u.d} ${h}${u.h}`;
  if (h > 0) return `${h}${u.h} ${m}${u.m}`;
  return `${m}${u.m}`;
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-1">
      <span className="text-muted text-sm">{label}</span>
      <span className="text-right font-semibold">{value}</span>
    </div>
  );
}

export default function Stats() {
  const [s, setS] = useState<StatsData | null>(null);
  const [logs, setLogs] = useState<LogInfo | null>(null);
  const [err, setErr] = useState("");
  const t = useT(kText);
  const lang = useLang();

  useEffect(() => {
    api
      .stats()
      .then(setS)
      .catch(() => setErr(t("failed")));
    // The log is its own request: a shop without one still shows its stats.
    api
      .logInfo()
      .then(setLogs)
      .catch(() => {});
    // Deliberately once per page load: the page itself says it is a snapshot.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (err) return <p className="text-red-600">{err}</p>;
  if (!s) return null;

  const srv = s.server;
  const shop = s.shop;
  const used = (total: number, free: number) =>
    `${bytes(total - free, lang)} / ${bytes(total, lang)}`;
  const units = { d: t("unitDay"), h: t("unitHour"), m: t("unitMinute") };

  return (
    <div className="form-page">
      <p className="hint">{t("hint")}</p>

      <section className="card flex flex-col gap-1">
        <h2 className="mb-2 font-bold">{t("shop")}</h2>
        <Row
          label={t("products")}
          value={`${shop.products} (${shop.products_visible} ${t("productsVisible")})`}
        />
        <Row
          label={t("orders")}
          value={`${shop.orders} (${shop.orders_new} ${t("ordersNew")})`}
        />
        <Row label={t("database")} value={bytes(shop.database_bytes, lang)} />
        <Row
          label={t("uploads")}
          value={`${bytes(shop.uploads_bytes, lang)} · ${shop.uploads_files} ${t("files")}`}
        />
        <Row
          label={t("process")}
          value={`${bytes(shop.process_rss_bytes, lang)} · ${shop.process_cpu.toFixed(1)}% · ${duration(shop.process_uptime, units)}`}
        />
        <Row label={t("version")} value={shop.version} />
      </section>

      <section className="card flex flex-col gap-1">
        <h2 className="font-bold">{t("server")}</h2>
        <p className="hint mb-2">{t("serverHint")}</p>
        <Row label={t("cpu")} value={`${srv.cpu.toFixed(1)}%`} />
        <Row label={t("load")} value={srv.load_average} />
        <Row
          label={t("memory")}
          value={`${used(srv.memory_total, srv.memory_free)} · ${bytes(srv.memory_free, lang)} ${t("free")}`}
        />
        <Row
          label={t("disk")}
          value={`${used(srv.hdd_total, srv.hdd_free)} · ${bytes(srv.hdd_free, lang)} ${t("free")}`}
        />
        <Row
          label={t("network")}
          value={`↓ ${bytes(srv.bandwidth_in, lang)}/s · ↑ ${bytes(srv.bandwidth_out, lang)}/s`}
        />
        <Row label={t("uptime")} value={duration(srv.uptime, units)} />
        <Row
          label={t("os")}
          value={`${srv.os.name} ${srv.os.arch} · ${srv.os.version}`}
        />
        {srv.vsystem && (
          <Row label={t("virt")} value={`${srv.vsystem} / ${srv.vrole}`} />
        )}
      </section>

      {/* Folded away on purpose: the log is for the day something looks wrong,
          not for the daily glance the rest of this page is. */}
      <details className="card">
        <summary className="cursor-pointer font-bold">{t("expert")}</summary>
        <p className="mt-2 text-sm text-gray-500">{t("expertHint")}</p>
        {logs?.available ? (
          <div className="mt-2 flex flex-col gap-1">
            <Row label={t("logSize")} value={bytes(logs.size, lang)} />
            <Row
              label={t("logModified")}
              value={new Date(logs.modified_at).toLocaleString(lang)}
            />
            <a
              className="mt-2 underline"
              href="/api/logs"
              target="_blank"
              rel="noopener"
            >
              {t("logOpen")}
            </a>
          </div>
        ) : (
          <p className="mt-2 text-sm text-gray-500">{t("logMissing")}</p>
        )}
      </details>
    </div>
  );
}
