import { type PriceRule } from "./api";
import { useT } from "./i18n";
import { toMinor, toRubles } from "./money";

const kText = {
  bandUpTo: { ru: "до", en: "up to" },
  bandAbove: { ru: "и выше", en: "and above" },
  bandMultiplier: { ru: "множитель", en: "multiplier" },
  addBand: { ru: "Добавить полосу", en: "Add a band" },
  removeBand: { ru: "Удалить", en: "Remove" },
};

// The open-ended "and above" row (up_to === 0) is what makes a ladder total:
// a price past the last band would otherwise have no multiplier at all, so
// adding a band keeps that row and keeps it last.
export default function PriceLadder({
  rules,
  onChange,
  newBand = { up_to: 100000, multiplier: 2 },
}: {
  rules: PriceRule[];
  onChange: (rules: PriceRule[]) => void;
  newBand?: PriceRule;
}) {
  const t = useT(kText);

  const patch = (i: number, next: Partial<PriceRule>) =>
    onChange(rules.map((x, j) => (j === i ? { ...x, ...next } : x)));

  const add = () =>
    onChange([
      ...rules.filter((r) => r.up_to !== 0),
      { ...newBand },
      rules.find((r) => r.up_to === 0) ?? { up_to: 0, multiplier: 1.5 },
    ]);

  return (
    <div className="flex flex-col gap-2">
      {rules.map((rule, i) => (
        <div key={i} className="flex items-center gap-2">
          <span className="hint w-12">{t("bandUpTo")}</span>
          {rule.up_to === 0 ? (
            <span className="w-28 font-semibold">{t("bandAbove")}</span>
          ) : (
            <input
              className="field w-28"
              inputMode="decimal"
              value={toRubles(rule.up_to)}
              onChange={(e) => patch(i, { up_to: toMinor(e.target.value) })}
            />
          )}
          <span className="hint">{t("bandMultiplier")}</span>
          <input
            className="field w-20"
            inputMode="decimal"
            value={String(rule.multiplier)}
            onChange={(e) =>
              patch(i, {
                multiplier: Number(e.target.value.replace(",", ".")) || 0,
              })
            }
          />
          <button
            className="text-muted cursor-pointer text-sm hover:text-red-600"
            onClick={() => onChange(rules.filter((_, j) => j !== i))}
          >
            {t("removeBand")}
          </button>
        </div>
      ))}
      <div>
        <button className="btn-ghost" onClick={add}>
          {t("addBand")}
        </button>
      </div>
    </div>
  );
}
