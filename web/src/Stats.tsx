import { useEffect, useState } from "react";
import { api, type Stats as StatsData } from "./api";
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
  cpu: { ru: "Процессор", en: "CPU" },
  load: { ru: "Средняя нагрузка", en: "Load average" },
  memory: { ru: "Память", en: "Memory" },
  disk: { ru: "Диск", en: "Disk" },
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
};

function bytes(n: number, lang: string): string {
  if (n <= 0) return "0";
  const units =
    lang === "ru"
      ? ["Б", "КБ", "МБ", "ГБ", "ТБ"]
      : ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(
    Math.floor(Math.log(n) / Math.log(1024)),
    units.length - 1,
  );
  const v = n / Math.pow(1024, i);
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

function duration(sec: number, lang: string): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const u =
    lang === "ru" ? { d: "д", h: "ч", m: "мин" } : { d: "d", h: "h", m: "m" };
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
  const [err, setErr] = useState("");
  const t = useT(kText);
  const lang = useLang();

  useEffect(() => {
    api
      .stats()
      .then(setS)
      .catch(() => setErr(t("failed")));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (err) return <p className="text-red-600">{err}</p>;
  if (!s) return null;

  const srv = s.server;
  const shop = s.shop;
  const used = (total: number, free: number) =>
    `${bytes(total - free, lang)} / ${bytes(total, lang)}`;

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
          value={`${bytes(shop.process_rss_bytes, lang)} · ${shop.process_cpu.toFixed(1)}% · ${duration(shop.process_uptime, lang)}`}
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
        <Row label={t("uptime")} value={duration(srv.uptime, lang)} />
        <Row
          label={t("os")}
          value={`${srv.os.name} ${srv.os.arch} · ${srv.os.version}`}
        />
        {srv.vsystem && (
          <Row label={t("virt")} value={`${srv.vsystem} / ${srv.vrole}`} />
        )}
      </section>
    </div>
  );
}
