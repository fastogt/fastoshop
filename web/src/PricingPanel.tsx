import { useEffect, useState } from "react";
import { api, apiError, type PriceRule, type Product } from "./api";
import { useT } from "./i18n";
import { toRubles } from "./money";
import PriceLadder from "./PriceLadder";
import { useSign } from "./shop";

const kText = {
  pricing: { ru: "Цены", en: "Prices" },
  pricingHint: {
    ru: "Как из цены поставщика получается цена на витрине. Цены, которые вы правили руками, пересчёт не трогает.",
    en: "How a supplier's price becomes the one on the shelf. Prices you edited by hand are left alone.",
  },
  rate: { ru: "Курс закупки", en: "Cost rate" },
  rateHint: {
    ru: "Во сколько раз цена поставщика превращается в вашу валюту. Прайс в той же валюте - оставьте 1.",
    en: "What the supplier's price is multiplied by to become your money. A price list in your own currency needs 1.",
  },
  markup: { ru: "Наценка, %", en: "Markup, %" },
  markupHint: {
    ru: "Сколько добавляем сверх закупки. 30 - это ×1.3.",
    en: "How much is added on top of the cost. 30 means ×1.3.",
  },
  bandsToggle: {
    ru: "Разная наценка для дешёвых и дорогих товаров",
    en: "A different markup for cheap and dear goods",
  },
  bandsHint: {
    ru: "На товаре за 7 рублей одна наценка ничего не оставляет, а на товаре за 300 выносит цену выше магазина бренда. Полоса - до какой закупочной цены какой множитель; последняя строка «и выше» обязательна.",
    en: 'One markup leaves nothing on a 7-rouble item and prices a 300-rouble one above the brand\'s own store. A band is: up to which cost, which multiplier; the final "and above" row is required.',
  },
  savePricing: { ru: "Сохранить", en: "Save" },
  pricingSaved: {
    ru: "Сохранено. Цены пока прежние - нажмите «Пересчитать цены».",
    en: 'Saved. No price has moved yet - press "Recompute prices".',
  },
  recompute: { ru: "Пересчитать цены", en: "Recompute prices" },
  recomputed: {
    ru: "Пересчитано товаров: {n}",
    en: "Products recomputed: {n}",
  },
  chainExample: {
    ru: "Закупка {source} → курс {rate} → {cost} → наценка → {shelf}",
    en: "Cost {source} → rate {rate} → {cost} → markup → {shelf}",
  },
  failed: {
    ru: "Не получилось. Обновите страницу и попробуйте ещё раз.",
    en: "That did not work. Reload the page and try again.",
  },
};

// The rate that carries a supplier's price into our money, and the markup on
// top. Kept next to the prices they produce, not on the Import tab.
export default function PricingPanel({
  sample,
  onRecomputed,
}: {
  // A row with a supplier price, to show the chain on real numbers.
  sample?: Product;
  onRecomputed: () => Promise<void>;
}) {
  const t = useT(kText);
  const sign = useSign();
  const [rate, setRate] = useState("1");
  const [rules, setRules] = useState<PriceRule[]>([]);
  const [priceMsg, setPriceMsg] = useState("");

  useEffect(() => {
    api.priceRules().then((r) => {
      setRate(String(r.coefficient || 1));
      setRules(r.rules);
    });
  }, []);

  // The percentage and the ladder are one thing stored one way: a single band
  // "and above" is a plain markup, several bands are a ladder - and no rules
  // at all is a 0% markup that was never set, not a hidden field.
  const markup =
    rules.length <= 1
      ? Math.round(((rules[0]?.multiplier ?? 1) - 1) * 100)
      : null;
  const savePricing = async (next: PriceRule[]) => {
    setPriceMsg("");
    try {
      const r = await api.setPriceRules(next);
      setRules(r.rules);
      setPriceMsg(t("pricingSaved"));
    } catch (e) {
      setPriceMsg(apiError(e) ?? t("failed"));
    }
  };
  const recompute = async () => {
    setPriceMsg("");
    try {
      // The server recomputes from the *stored* ladder, so what is on the
      // screen is saved first - otherwise the button reprices the catalogue
      // by numbers the owner just replaced and reports success.
      await api.setPriceRules(rules);
      const r = await api.recomputePrices(Number(rate.replace(",", ".")) || 1);
      setPriceMsg(t("recomputed", { n: r.updated }));
      await onRecomputed();
    } catch (e) {
      setPriceMsg(apiError(e) ?? t("failed"));
    }
  };

  return (
    // Folded away: pricing is set once and then left alone, while the table
    // below is the daily work. Open, it answers the one question the numbers
    // in the price column raise - where they came from.
    <details className="card mb-5">
      <summary className="cursor-pointer font-bold">{t("pricing")}</summary>
      <p className="hint mt-2">{t("pricingHint")}</p>

      <div className="mt-3 flex flex-wrap items-start gap-6">
        <div>
          <label className="label">{t("rate")}</label>
          <input
            className="field w-32"
            inputMode="decimal"
            value={rate}
            onChange={(e) => setRate(e.target.value)}
          />
          <p className="hint mt-1 max-w-xs">{t("rateHint")}</p>
        </div>

        {markup !== null && (
          <div>
            <label className="label">{t("markup")}</label>
            <input
              className="field w-24"
              inputMode="decimal"
              value={String(markup)}
              onChange={(e) =>
                setRules([
                  {
                    up_to: 0,
                    multiplier:
                      1 + (Number(e.target.value.replace(",", ".")) || 0) / 100,
                  },
                ])
              }
            />
            <p className="hint mt-1 max-w-xs">{t("markupHint")}</p>
          </div>
        )}
      </div>

      {/* The chain on a real row: three arrows say more than a paragraph, and
          they show that the number in the price column is not an accident. */}
      {sample?.source_price ? (
        <p className="hint mt-3">
          {t("chainExample", {
            source: String(toRubles(sample.source_price)),
            rate,
            cost: `${toRubles(Math.round(sample.source_price * (Number(rate.replace(",", ".")) || 1)))} ${sign}`,
            shelf: `${toRubles(sample.price ?? 0)} ${sign}`,
          })}
        </p>
      ) : null}

      {/* The editor follows the data, not a toggle of its own: bands on
          screen mean bands in the ladder, and deleting down to one row
          brings the plain percent field back. A hidden multi-band ladder
          would still be saved and still price the catalogue. */}
      {rules.length <= 1 && (
        <div className="mt-4">
          <button
            className="text-brand cursor-pointer text-sm underline"
            onClick={() =>
              setRules([
                { up_to: 5000, multiplier: 1.5 },
                ...(rules.length ? rules : [{ up_to: 0, multiplier: 1.3 }]),
              ])
            }
          >
            {t("bandsToggle")}
          </button>
        </div>
      )}

      {rules.length > 1 && (
        <div className="border-line mt-3 flex flex-col gap-2 border-t pt-3">
          <p className="hint max-w-2xl">{t("bandsHint")}</p>
          <PriceLadder
            rules={rules}
            onChange={setRules}
            newBand={{ up_to: 5000, multiplier: 1.5 }}
          />
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-3">
        <button className="btn-ghost" onClick={() => void savePricing(rules)}>
          {t("savePricing")}
        </button>
        <button className="btn" onClick={() => void recompute()}>
          {t("recompute")}
        </button>
        {priceMsg && <span className="text-green-700">{priceMsg}</span>}
      </div>
    </details>
  );
}
