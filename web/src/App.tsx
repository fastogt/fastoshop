import { useEffect, useState } from "react";
import { api } from "./api";
import { setLang, useLang, useT } from "./i18n";
import { loadShop } from "./shop";
import Setup from "./Setup";
import Invite from "./Invite";
import Login from "./Login";
import Products from "./Products";
import Categories from "./Categories";
import Orders from "./Orders";
import Profile from "./Profile";
import Stats from "./Stats";
import Import from "./Import";
import Ozon from "./Ozon";
import WB from "./WB";

type Screen = "loading" | "setup" | "invite" | "login" | "app";
type Tab =
  | "products"
  | "categories"
  | "orders"
  | "profile"
  | "import"
  | "ozon"
  | "wb"
  | "stats";

const kText = {
  products: { ru: "Товары", en: "Products" },
  categories: { ru: "Категории", en: "Categories" },
  orders: { ru: "Заказы", en: "Orders" },
  import: { ru: "Импорт", en: "Import" },
  ozon: { ru: "Ozon", en: "Ozon" },
  wb: { ru: "Wildberries", en: "Wildberries" },
  stats: { ru: "Статистика", en: "Stats" },
  profile: { ru: "Профиль", en: "Profile" },
  openShop: { ru: "Открыть магазин ↗", en: "Open shop ↗" },
  logout: { ru: "Выйти", en: "Log out" },
  source: {
    ru: "FastoShop — открытый код, AGPL-3.0",
    en: "FastoShop — open source, AGPL-3.0",
  },
};

const kTabs: Tab[] = [
  "products",
  "categories",
  "orders",
  "import",
  "ozon",
  "wb",
  "stats",
  "profile",
];

export default function App() {
  const [screen, setScreen] = useState<Screen>("loading");
  const [tab, setTab] = useState<Tab>("products");
  const t = useT(kText);
  const lang = useLang();

  // The invite link arrives by email and leads straight here: the owner is
  // already created with the shop, but has no password yet.
  const [invite] = useState(
    () => new URLSearchParams(window.location.search).get("invite") ?? "",
  );

  useEffect(() => {
    if (invite) {
      setScreen("invite");
      return;
    }
    api.setupNeeded().then(({ needed }) => {
      if (needed) {
        setScreen("setup");
        return;
      }
      api
        .products()
        .then(() => {
          loadShop();
          setScreen("app");
        })
        .catch(() => setScreen("login"));
    });
  }, [invite]);

  if (screen === "loading") return null;
  // enter() and not setScreen: the shop's currency and the owner's language come
  // from the server, and until they are loaded the admin shows roubles and the
  // browser's language. Reaching the app through a login used to skip that.
  const enter = () => {
    loadShop();
    setScreen("app");
  };

  if (screen === "setup") return <Setup onDone={enter} />;
  if (screen === "invite")
    return (
      <Invite
        token={invite}
        onDone={() => {
          window.history.replaceState({}, "", "/admin/");
          enter();
        }}
      />
    );
  if (screen === "login") return <Login onDone={enter} />;

  const logout = () => {
    api.logout().finally(() => setScreen("login"));
  };

  return (
    <div className="min-h-screen">
      <header className="border-line border-b bg-white">
        <div className="page flex items-center gap-1">
          <span className="mr-6 text-lg font-extrabold tracking-tight">
            FastoShop
          </span>
          {kTabs.map((k) => (
            <button
              key={k}
              onClick={() => setTab(k)}
              className={
                "-mb-px border-b-2 px-3 py-4 text-sm font-semibold transition-colors " +
                (tab === k
                  ? "border-brand text-brand"
                  : "text-muted hover:text-ink border-transparent")
              }
            >
              {t(k)}
            </button>
          ))}
          <a
            href="/"
            target="_blank"
            rel="noreferrer"
            className="text-muted hover:text-ink ml-auto text-sm"
          >
            {t("openShop")}
          </a>
          <button
            onClick={() => setLang(lang === "ru" ? "en" : "ru")}
            className="text-muted hover:text-ink ml-4 text-sm font-semibold uppercase"
          >
            {lang === "ru" ? "EN" : "RU"}
          </button>
          <button
            onClick={logout}
            className="text-muted hover:text-ink ml-4 text-sm"
          >
            {t("logout")}
          </button>
        </div>
      </header>
      <main className="page py-8">
        {tab === "products" && <Products />}
        {tab === "categories" && <Categories />}
        {tab === "orders" && <Orders />}
        {tab === "profile" && <Profile />}
        {tab === "import" && <Import />}
        {tab === "ozon" && <Ozon />}
        {tab === "wb" && <WB />}
        {tab === "stats" && <Stats />}
      </main>
      {/* AGPL §13: whoever is offered the shop as a service must have access to
          the sources. The footer link is that offer; without it, hosting our
          own product would violate our own license. */}
      <footer className="border-line text-muted border-t py-4 text-sm">
        <div className="page">
          <a
            href="https://github.com/fastogt/fastoshop"
            target="_blank"
            rel="noreferrer"
            className="hover:text-ink"
          >
            {t("source")}
          </a>
        </div>
      </footer>
    </div>
  );
}
