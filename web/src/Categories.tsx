import { useEffect, useMemo, useState } from "react";
import { api, apiError, type CategoryNode } from "./api";
import { useT } from "./i18n";

const kText = {
  title: { ru: "Категории", en: "Categories" },
  intro: {
    ru: "Разделы витрины: у каждого своя страница, свой заголовок и свой текст. Дерево ведёте вы — часть разделов приезжает с импортом, остальное заводится руками.",
    en: "The storefront sections: each has its own page, heading and text. You keep the tree yourself — some of it arrives with an import, the rest you create by hand.",
  },
  search: { ru: "Поиск по дереву", en: "Search the tree" },
  addRoot: { ru: "+ категория", en: "+ category" },
  addChild: { ru: "+ подкатегория", en: "+ subcategory" },
  namePrompt: { ru: "Название категории", en: "Category name" },
  empty: {
    ru: "Категорий пока нет. Заведите первую или загрузите каталог с категориями на вкладке «Импорт».",
    en: "No categories yet. Create the first one, or import a catalogue that has them.",
  },
  pick: {
    ru: "Выберите категорию слева, чтобы отредактировать её.",
    en: "Pick a category on the left to edit it.",
  },
  name: { ru: "Название", en: "Name" },
  parent: { ru: "Родитель", en: "Parent" },
  root: { ru: "— верхний уровень —", en: "— top level —" },
  order: { ru: "Порядок", en: "Order" },
  up: { ru: "Выше", en: "Up" },
  down: { ru: "Ниже", en: "Down" },
  hidden: { ru: "Скрыть с витрины", en: "Hide from the storefront" },
  hiddenHint: {
    ru: "Скрытый раздел и всё, что внутри, исчезает из каталога, карты сайта и поиска. Товары продолжают продаваться по прямым ссылкам.",
    en: "A hidden section and everything inside it disappears from the catalogue, the sitemap and search. The goods still sell by direct link.",
  },
  body: { ru: "Текст страницы", en: "Page text" },
  bodyHint: {
    ru: "Что здесь продаётся, кому подходит, чем отличается — своими словами. Первые предложения станут описанием страницы для поисковика. Пустое поле — блока нет.",
    en: "What is sold here, who it suits, what makes it different — in your own words. The first sentences become the page description for search engines. An empty field means no block.",
  },
  save: { ru: "Сохранить", en: "Save" },
  remove: { ru: "Удалить категорию", en: "Delete category" },
  saved: { ru: "Сохранено", en: "Saved" },
  saveFailed: { ru: "Не сохранилось", en: "Could not save" },
  goods: { ru: "товаров", en: "products" },
  confirmDelete: {
    ru: "Удалить «{name}»? Товары ({n}) и подкатегории переедут в раздел выше, старый адрес будет перенаправлен. Отменить это нельзя.",
    en: 'Delete "{name}"? Its products ({n}) and subcategories move to the section above and the old address gets redirected. This cannot be undone.',
  },
};

// A node as the panel draws it: the stored path plus the depth and the name,
// which are the path read out.
interface TreeItem extends CategoryNode {
  depth: number;
  name: string;
  hasChildren: boolean;
}

const kSep = "/";
const leaf = (path: string) => path.slice(path.lastIndexOf(kSep) + 1);
const parentOf = (path: string) =>
  path.includes(kSep) ? path.slice(0, path.lastIndexOf(kSep)) : "";

export default function Categories() {
  const t = useT(kText);
  const [nodes, setNodes] = useState<CategoryNode[]>([]);
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<string | null>(null);
  const [form, setForm] = useState<CategoryNode | null>(null);
  const [msg, setMsg] = useState("");

  const reload = async (keep?: string) => {
    const res = await api.categoryTree();
    setNodes(res.categories);
    const path = keep ?? selected;
    const node = res.categories.find((n) => n.path === path);
    setSelected(node ? node.path : null);
    setForm(node ? { ...node } : null);
  };

  useEffect(() => {
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Search opens the tree down to whatever matched: a hit five levels deep is
  // useless while its parents stay folded.
  const matching = useMemo(() => {
    if (!q.trim()) return null;
    const needle = q.trim().toLowerCase();
    const hit = new Set<string>();
    for (const n of nodes) {
      if (!n.path.toLowerCase().includes(needle)) continue;
      const segments = n.path.split(kSep);
      for (let i = 0; i < segments.length; i++) {
        hit.add(segments.slice(0, i + 1).join(kSep));
      }
    }
    return hit;
  }, [q, nodes]);

  const items: TreeItem[] = useMemo(() => {
    const withChildren = new Set(
      nodes.map((n) => parentOf(n.path)).filter(Boolean),
    );
    return nodes
      .filter((n) => {
        if (matching) return matching.has(n.path);
        const parent = parentOf(n.path);
        // Roots always; a child only while every ancestor is unfolded.
        return (
          !parent ||
          parent
            .split(kSep)
            .every((_, i, all) => open.has(all.slice(0, i + 1).join(kSep)))
        );
      })
      .map((n) => ({
        ...n,
        depth: n.path.split(kSep).length - 1,
        name: leaf(n.path),
        hasChildren: withChildren.has(n.path),
      }));
  }, [nodes, open, matching]);

  const toggle = (path: string) => {
    const next = new Set(open);
    if (!next.delete(path)) next.add(path);
    setOpen(next);
  };

  const add = async (parent: string) => {
    const name = window.prompt(t("namePrompt"), "");
    if (!name?.trim()) return;
    try {
      const res = await api.createCategory(parent, name.trim());
      if (parent) setOpen(new Set(open).add(parent));
      await reload(res.path);
    } catch (e) {
      setMsg(apiError(e) ?? t("saveFailed"));
    }
  };

  const save = async () => {
    if (!form || !selected) return;
    try {
      const res = await api.updateCategory(selected, {
        name: leaf(form.path),
        parent: parentOf(form.path),
        hidden: form.hidden,
        body: form.body,
      });
      setMsg(t("saved"));
      await reload(res.path);
    } catch (e) {
      setMsg(apiError(e) ?? t("saveFailed"));
    }
  };

  // Moving is a swap of positions with the neighbour: the pair is renumbered
  // from where they stand now, so a shop that never touched the order needs no
  // migration for one.
  const move = async (dir: -1 | 1) => {
    if (!form) return;
    const siblings = nodes.filter(
      (n) => parentOf(n.path) === parentOf(form.path),
    );
    const at = siblings.findIndex((n) => n.path === form.path);
    const to = at + dir;
    if (at < 0 || to < 0 || to >= siblings.length) return;
    try {
      await api.updateCategory(siblings[to].path, { position: at });
      await api.updateCategory(form.path, { position: to });
      await reload(form.path);
    } catch (e) {
      setMsg(apiError(e) ?? t("saveFailed"));
    }
  };

  const remove = async () => {
    if (!form) return;
    const text = t("confirmDelete", { name: leaf(form.path), n: form.count });
    if (!window.confirm(text)) return;
    try {
      await api.deleteCategory(form.path);
      setSelected(null);
      setForm(null);
      await reload("");
    } catch (e) {
      setMsg(apiError(e) ?? t("saveFailed"));
    }
  };

  return (
    <div>
      <h2 className="text-xl font-semibold">{t("title")}</h2>
      <p className="hint mt-1 mb-4 max-w-3xl">{t("intro")}</p>
      <div className="flex flex-col gap-6 lg:flex-row">
        <div className="border-line rounded border p-3 lg:w-96 lg:shrink-0">
          <input
            className="field mb-3"
            placeholder={t("search")}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          {nodes.length === 0 && <p className="hint">{t("empty")}</p>}
          <ul className="max-h-[32rem] overflow-y-auto">
            {items.map((n) => (
              <li key={n.path} style={{ paddingLeft: n.depth * 16 }}>
                <div className="flex items-center gap-1 py-0.5">
                  <button
                    className="text-muted w-4 shrink-0"
                    onClick={() => n.hasChildren && toggle(n.path)}
                  >
                    {n.hasChildren ? (open.has(n.path) ? "▾" : "▸") : ""}
                  </button>
                  <button
                    className={`flex-1 truncate text-left ${
                      selected === n.path ? "text-brand font-semibold" : ""
                    } ${n.hidden ? "text-muted line-through" : ""}`}
                    onClick={() => {
                      setSelected(n.path);
                      setForm({ ...n });
                      setMsg("");
                    }}
                    title={n.path}
                  >
                    {n.name}
                  </button>
                  <span className="text-muted shrink-0 text-xs">{n.count}</span>
                  <button
                    className="text-muted shrink-0 px-1 text-xs"
                    title={t("addChild")}
                    onClick={() => void add(n.path)}
                  >
                    +
                  </button>
                </div>
              </li>
            ))}
          </ul>
          <button className="btn-ghost mt-2" onClick={() => void add("")}>
            {t("addRoot")}
          </button>
        </div>

        <div className="flex-1">
          {!form ? (
            <p className="hint">{t("pick")}</p>
          ) : (
            <div className="flex flex-col gap-4">
              <div>
                <label className="label">{t("name")}</label>
                <input
                  className="field"
                  value={leaf(form.path)}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      path: [parentOf(form.path), e.target.value]
                        .filter(Boolean)
                        .join(kSep),
                    })
                  }
                />
              </div>
              <div>
                <label className="label">{t("parent")}</label>
                <select
                  className="field"
                  value={parentOf(form.path)}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      path: [e.target.value, leaf(form.path)]
                        .filter(Boolean)
                        .join(kSep),
                    })
                  }
                >
                  <option value="">{t("root")}</option>
                  {nodes
                    .filter(
                      (n) =>
                        n.path !== selected &&
                        !n.path.startsWith(selected + kSep),
                    )
                    .map((n) => (
                      <option key={n.path} value={n.path}>
                        {n.path}
                      </option>
                    ))}
                </select>
              </div>
              <div className="flex items-center gap-3">
                <span className="label mb-0">{t("order")}</span>
                <button className="btn-ghost" onClick={() => void move(-1)}>
                  ↑ {t("up")}
                </button>
                <button className="btn-ghost" onClick={() => void move(1)}>
                  ↓ {t("down")}
                </button>
              </div>
              <div>
                <label className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    checked={form.hidden}
                    onChange={(e) =>
                      setForm({ ...form, hidden: e.target.checked })
                    }
                  />
                  <span>{t("hidden")}</span>
                </label>
                <p className="hint mt-1">{t("hiddenHint")}</p>
              </div>
              <div>
                <label className="label">{t("body")}</label>
                <textarea
                  className="field min-h-40"
                  autoComplete="off"
                  value={form.body}
                  onChange={(e) => setForm({ ...form, body: e.target.value })}
                />
                <p className="hint mt-1">{t("bodyHint")}</p>
              </div>
              <div className="flex flex-wrap items-center gap-3">
                <button className="btn" onClick={() => void save()}>
                  {t("save")}
                </button>
                <button
                  className="btn-ghost text-red-600"
                  onClick={() => void remove()}
                >
                  {t("remove")}
                </button>
                <span className="text-muted text-sm">
                  {form.count} {t("goods")}
                </span>
                {msg && <span className="text-sm">{msg}</span>}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
