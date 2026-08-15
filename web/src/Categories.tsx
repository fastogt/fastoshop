import { useEffect, useState } from "react";
import { api, apiError, type CategoryNode } from "./api";
import DataTable, { type Column } from "./DataTable";
import Modal from "./Modal";
import { useT } from "./i18n";

const kText = {
  title: { ru: "Категории", en: "Categories" },
  intro: {
    ru: "Дерево собирается из товаров: каждый узел — отдельная страница витрины. Текст под заголовком превращает список товаров в посадочную страницу — то, ради чего магазины держат отдельные лендинги.",
    en: "The tree is derived from the products: every node is a storefront page of its own. The text under the heading turns a listing into a landing page — the thing shops keep separate pages for.",
  },
  search: { ru: "Поиск по категориям", en: "Search categories" },
  colPath: { ru: "Категория", en: "Category" },
  colCount: { ru: "Товаров", en: "Products" },
  colText: { ru: "Текст", en: "Text" },
  has: { ru: "есть", en: "yes" },
  none: { ru: "нет", en: "no" },
  edit: { ru: "Текст категории", en: "Category text" },
  bodyHint: {
    ru: "Что здесь продаётся, кому подходит, чем отличается — своими словами. Первые предложения станут описанием страницы для поисковика; переводы строк сохраняются. Пустое поле — блока нет.",
    en: "What is sold here, who it suits, what makes it different — in your own words. The first sentences become the page description for search engines; line breaks are kept. An empty field means no block.",
  },
  save: { ru: "Сохранить", en: "Save" },
  draft: { ru: "Черновик", en: "Draft" },
  draftHint: {
    ru: "Соберём заготовку из ваших же данных: что в категории, сколько позиций, разброс цен и условия доставки из профиля. Ничего не публикуется — правьте под себя и сохраняйте.",
    en: "We assemble a starting text from your own data: what is in the category, how many items, the price range and the delivery terms from your profile. Nothing is published — edit it and save.",
  },
  remove: { ru: "Удалить текст", en: "Delete text" },
  draftFailed: { ru: "Черновик не собрался", en: "Could not build a draft" },
  saveFailed: { ru: "Не сохранилось", en: "Could not save" },
  empty: {
    ru: "Категорий пока нет. Они приезжают с импортом или ставятся в карточке товара.",
    en: "No categories yet. They arrive with an import or are set on a product.",
  },
};

export default function Categories() {
  const t = useT(kText);
  const [rows, setRows] = useState<CategoryNode[]>([]);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const [edit, setEdit] = useState<CategoryNode | null>(null);
  const [body, setBody] = useState("");
  const [msg, setMsg] = useState("");

  const reload = async () => {
    const res = await api.categoryTree(q || undefined);
    setRows(res.categories);
  };

  useEffect(() => {
    void reload();
    // Search is applied on the server; the page returns to the first one so the
    // owner is not left looking at page 7 of a shorter list.
    setPage(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q]);

  const draft = async () => {
    if (!edit) return;
    try {
      const res = await api.categoryDraft(edit.path);
      setBody(res.body);
    } catch (e) {
      setMsg(apiError(e) ?? t("draftFailed"));
    }
  };

  const remove = async () => {
    if (!edit) return;
    try {
      await api.deleteCategoryText(edit.path);
      setEdit(null);
      await reload();
    } catch (e) {
      setMsg(apiError(e) ?? t("saveFailed"));
    }
  };

  const save = async () => {
    if (!edit) return;
    try {
      await api.setCategoryText(edit.path, body);
      setEdit(null);
      await reload();
    } catch (e) {
      setMsg(apiError(e) ?? t("saveFailed"));
    }
  };

  const columns: Column<CategoryNode>[] = [
    {
      key: "path",
      label: t("colPath"),
      render: (r) => (
        <button
          className="link text-left"
          onClick={() => {
            setEdit(r);
            setBody(r.body);
            setMsg("");
          }}
        >
          {r.path}
        </button>
      ),
    },
    {
      key: "count",
      label: t("colCount"),
      width: "6rem",
      render: (r) => r.count,
    },
    {
      key: "body",
      label: t("colText"),
      width: "6rem",
      hideMobile: true,
      render: (r) => (
        <span className={r.body ? "" : "text-muted"}>
          {r.body ? t("has") : t("none")}
        </span>
      ),
    },
  ];

  // The tree is hundreds of nodes, not thousands, so it arrives whole and the
  // page is cut here. Products are the list that needs the server for that.
  const shown = rows.slice((page - 1) * pageSize, page * pageSize);

  return (
    <div>
      <h2 className="text-xl font-semibold">{t("title")}</h2>
      <p className="hint mt-1 mb-4 max-w-3xl">{t("intro")}</p>
      <input
        className="field mb-4 max-w-sm"
        placeholder={t("search")}
        value={q}
        onChange={(e) => setQ(e.target.value)}
      />
      {rows.length === 0 ? (
        <p className="hint">{t("empty")}</p>
      ) : (
        <DataTable
          columns={columns}
          rows={shown}
          rowId={(r) => r.path}
          emptyTitle={t("empty")}
          total={rows.length}
          page={page}
          pageSize={pageSize}
          onPage={setPage}
          onPageSize={(n) => {
            setPageSize(n);
            setPage(1);
          }}
        />
      )}
      {edit && (
        <Modal
          title={`${t("edit")}: ${edit.path}`}
          onClose={() => setEdit(null)}
          footer={
            <div className="flex w-full items-center gap-3">
              <button className="btn-ghost" onClick={() => void draft()}>
                {t("draft")}
              </button>
              {edit.body && (
                <button
                  className="btn-ghost text-red-600"
                  onClick={() => void remove()}
                >
                  {t("remove")}
                </button>
              )}
              <button className="btn ml-auto" onClick={() => void save()}>
                {t("save")}
              </button>
            </div>
          }
        >
          <textarea
            className="field min-h-48"
            autoComplete="off"
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
          <p className="hint mt-1">{t("bodyHint")}</p>
          <p className="hint mt-1">{t("draftHint")}</p>
          {msg && <p className="mt-2 text-red-600">{msg}</p>}
        </Modal>
      )}
    </div>
  );
}
