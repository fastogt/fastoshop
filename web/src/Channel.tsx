import { type Warehouse } from "./api";
import { useT } from "./i18n";

const kChannelTabs = [
  "tabSetup",
  "tabPublish",
  "tabPrices",
  "tabSales",
] as const;
export type ChannelTab = (typeof kChannelTabs)[number];

const kText = {
  tabSetup: { ru: "Подключение", en: "Connection" },
  tabPublish: { ru: "Публикация", en: "Publishing" },
  tabPrices: { ru: "Цены", en: "Prices" },
  tabSales: { ru: "Продажи", en: "Sales" },
  pickWarehouse: { ru: "- выберите склад -", en: "- pick a warehouse -" },
  loadWarehouses: { ru: "Загрузить склады", en: "Load warehouses" },
};

export function ChannelTabs({
  active,
  onSelect,
}: {
  active: ChannelTab;
  onSelect: (tab: ChannelTab) => void;
}) {
  const t = useT(kText);
  return (
    <div className="border-line flex flex-wrap gap-1 border-b">
      {kChannelTabs.map((k) => (
        <button
          key={k}
          onClick={() => onSelect(k)}
          className={
            "-mb-px border-b-2 px-3 py-2 text-sm font-semibold transition-colors " +
            (active === k
              ? "border-brand text-brand"
              : "text-muted hover:text-ink border-transparent")
          }
        >
          {t(k)}
        </button>
      ))}
    </div>
  );
}

// The list is a convenience, not an obligation: until it is loaded, or when
// the platform answers with nothing, the id is typed by hand.
export function WarehousePicker({
  name,
  label,
  hint,
  value,
  onChange,
  warehouses,
  onLoad,
  busy,
}: {
  name: string;
  label: string;
  hint: string;
  value: string;
  onChange: (id: string) => void;
  warehouses: Warehouse[] | null;
  onLoad: () => void;
  busy: boolean;
}) {
  const t = useT(kText);
  return (
    <div>
      <label className="label" htmlFor={name}>
        {label}
      </label>
      <div className="flex items-center gap-3">
        {warehouses && warehouses.length > 0 ? (
          <select
            id={name}
            className="field"
            name={name}
            autoComplete="off"
            value={value}
            onChange={(e) => onChange(e.target.value)}
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
            id={name}
            className="field"
            name={name}
            autoComplete="off"
            value={value}
            onChange={(e) => onChange(e.target.value)}
          />
        )}
        <button className="btn-ghost" disabled={busy} onClick={onLoad}>
          {t("loadWarehouses")}
        </button>
      </div>
      <p className="hint mt-1">{hint}</p>
    </div>
  );
}
