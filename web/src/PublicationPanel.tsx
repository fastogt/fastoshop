import { useMemo, type ReactNode } from "react";
import {
  type CabinetState,
  type Candidate,
  type CandidateView,
  type UnlinkedProduct,
} from "./api";
import DataTable from "./DataTable";
import { IconDownload, IconUpload } from "./Icons";
import { useT } from "./i18n";

const kText = {
  publication: { ru: "Публикация", en: "Publication" },
  cabinetSummary: {
    // Only the cabinet's own card count: the states below it are on the
    // buttons, where they belong - the owner is choosing what to look at, and a
    // sentence repeating the choice word for word is noise between them.
    ru: "В кабинете карточек: {cards}.",
    en: "Cards in the cabinet: {cards}.",
  },
  viewReady: { ru: "Можно связать", en: "Ready to link" },
  viewLinked: { ru: "Связано", en: "Linked" },
  viewNoCard: { ru: "Нет карточки", en: "No card" },
  viewAll: { ru: "Все товары", en: "All products" },
  searchProducts: {
    ru: "Поиск по названию или артикулу",
    en: "Search by title or article",
  },
  colProduct: { ru: "Товар", en: "Product" },
  colArticle: { ru: "Артикул", en: "Article" },
  colStock: { ru: "Остаток", en: "Stock" },
  colPublished: { ru: "На площадке", en: "On the platform" },
  yes: { ru: "да", en: "yes" },
  no: { ru: "нет", en: "no" },
  stateReady: { ru: "можно связать", en: "ready to link" },
  stateNoCard: { ru: "нет карточки", en: "no card" },
  hiddenBadge: { ru: "скрыт с витрины", en: "hidden from storefront" },
  publish: { ru: "Опубликовать", en: "Publish" },
  unpublish: { ru: "Снять с публикации", en: "Unpublish" },
  noCandidates: {
    ru: "Товаров нет. Заведите их вручную или перенесите каталог на вкладке «Импорт».",
    en: "No products yet. Add them by hand or bring a catalogue over on the Import tab.",
  },
  noCardTitle: { ru: "Карточка не найдена", en: "No card found" },
  noCardHint: {
    ru: "Эти товары не нашлись в кабинете по артикулу. Заведите карточку на площадке с тем же артикулом и повторите.",
    en: "These products had no article match in the cabinet. Create the card on the platform with the same article and try again.",
  },
  zeroFailedTitle: {
    ru: "Не удалось обнулить остаток",
    en: "Could not zero the stock",
  },
  zeroFailedHint: {
    ru: "Связь сохранена намеренно: пока на площадке остаётся наш остаток, забывать про карточку нельзя - она продолжит продавать.",
    en: "The link is kept on purpose: while our stock is still live on the platform, forgetting the card would let it keep selling.",
  },
  articleEmpty: { ru: "не задан", en: "not set" },
  orphans: {
    ru: "Есть в кабинете, нет в магазине",
    en: "In the account, not in the shop",
  },
  orphansHint: {
    ru: "Эти карточки не совпали ни с одним товаром - их можно перенести на вкладке «Импорт».",
    en: "These cards matched no product - you can bring them over on the Import tab.",
  },
  orphansMore: { ru: "…и ещё {n}", en: "…and {n} more" },
};

type Kind = CandidateView["kind"];

function UnlinkedList({
  title,
  hint,
  rows,
  danger,
  empty,
}: {
  title: string;
  hint: string;
  rows: UnlinkedProduct[];
  danger?: boolean;
  empty: string;
}) {
  if (rows.length === 0) return null;
  return (
    <div>
      <h3 className={"font-semibold" + (danger ? " text-red-600" : "")}>
        {title}
      </h3>
      <p className="hint">{hint}</p>
      <ul className="mt-2 flex flex-col gap-1">
        {rows.map((p) => (
          <li key={p.id} className="text-sm">
            {p.sku || empty} - {p.title}
            {p.reason && ` (${p.reason})`}
          </li>
        ))}
      </ul>
    </div>
  );
}

// The part of a channel tab that does not depend on the platform: which
// products to offer it, and what it answered. The cabinet call behind `cabinet`
// stays with the caller - so does deciding what the slices default to.
export default function PublicationPanel({
  hint,
  summaryExtra,
  cabinet,
  view,
  onView,
  search,
  onSearch,
  candidates,
  total,
  page,
  pageSize,
  onPage,
  onPublish,
  onUnpublish,
  message,
  noCard,
  zeroFailed,
}: {
  hint?: string;
  // A platform-specific sentence after the card count (WB: ambiguous cards).
  summaryExtra?: string;
  cabinet: CabinetState | null;
  view: Kind;
  onView: (kind: Kind) => void;
  search: string;
  onSearch: (q: string) => void;
  candidates: Candidate[];
  total: number;
  page: number;
  pageSize: number;
  onPage: (page: number) => void;
  onPublish: (ids: number[]) => void;
  onUnpublish: (ids: number[]) => void;
  message: ReactNode;
  noCard: UnlinkedProduct[];
  zeroFailed: UnlinkedProduct[];
}) {
  const t = useT(kText);
  const ready = useMemo(() => new Set(cabinet?.ready_ids ?? []), [cabinet]);

  return (
    <>
      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("publication")}</h2>
          {hint && <p className="hint">{hint}</p>}
          {cabinet && (
            <p className="hint mt-1">
              {t("cabinetSummary", { cards: cabinet.cards })}
              {summaryExtra && " " + summaryExtra}
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
                onClick={() => onView(kind)}
                className={
                  "rounded-full border px-3 py-1 text-sm " +
                  (view === kind
                    ? "border-brand text-brand font-semibold"
                    : "border-line text-muted")
                }
              >
                {label} {n}
              </button>
            ))}
          </div>
        )}
        <input
          className="field w-64"
          placeholder={t("searchProducts")}
          value={search}
          onChange={(e) => onSearch(e.target.value)}
        />

        <DataTable<Candidate>
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
              render: (p) => p.sku || "-",
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
          rows={candidates}
          rowId={(p) => p.product_id}
          total={total}
          page={page}
          pageSize={pageSize}
          onPage={onPage}
          selectable
          // Which goods go to a marketplace is a decision, and a filter is not
          // one; publishing also matches SKUs against the cabinet's card list,
          // and "everything matching" would sweep the whole catalogue through
          // it.
          allowAll={false}
          bulkActions={[
            {
              label: t("publish"),
              icon: <IconUpload />,
              idsOnly: true,
              onClick: (sel) => onPublish(sel.ids),
            },
            {
              label: t("unpublish"),
              icon: <IconDownload />,
              danger: true,
              idsOnly: true,
              onClick: (sel) => onUnpublish(sel.ids),
            },
          ]}
          emptyTitle={t("noCandidates")}
        />

        {message}

        <UnlinkedList
          title={t("noCardTitle")}
          hint={t("noCardHint")}
          rows={noCard}
          empty={t("articleEmpty")}
        />
        <UnlinkedList
          title={t("zeroFailedTitle")}
          hint={t("zeroFailedHint")}
          rows={zeroFailed}
          danger
          empty={t("articleEmpty")}
        />
      </section>

      {/* Cards in the cabinet with no product of ours: the cabinet call the tab
          already makes on open answers it for free. */}
      {!!cabinet?.orphan_skus?.length && (
        <section className="card flex flex-col gap-2">
          <div>
            <h2 className="font-bold">{t("orphans")}</h2>
            <p className="hint">{t("orphansHint")}</p>
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
              {t("orphansMore", {
                n: cabinet.orphans - cabinet.orphan_skus.length,
              })}
            </p>
          )}
        </section>
      )}
    </>
  );
}
