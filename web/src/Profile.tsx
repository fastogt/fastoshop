import { useEffect, useState } from "react";
import { api, type Settings } from "./api";
import { useT } from "./i18n";
import { setCurrency } from "./shop";

const kText = {
  title: { ru: "Профиль", en: "Profile" },
  shop: { ru: "Магазин", en: "Shop" },
  shopHint: {
    ru: "Название и телефон видны покупателям на витрине.",
    en: "The name and phone number are shown to customers on the storefront.",
  },
  shopName: { ru: "Название магазина", en: "Shop name" },
  shopNamePlaceholder: { ru: "Лавка Ивана", en: "Ivan's Shop" },
  shopPhone: { ru: "Телефон для покупателей", en: "Customer phone number" },
  telegram: { ru: "Telegram для заказов", en: "Telegram for orders" },
  whatsapp: { ru: "WhatsApp для заказов", en: "WhatsApp for orders" },
  messengerHint: {
    ru: "Необязательно. Если заполнить, в карточке товара появится кнопка: покупатель нажимает и попадает в переписку с уже готовым текстом — название, артикул, цена и ссылка. Логин можно вставить как «@myshop» или ссылкой целиком.",
    en: 'Optional. Filled in, it adds a button to the product page: the buyer taps it and lands in a chat with the message already written — title, article, price and a link. Paste the handle as "@myshop" or as a full link.',
  },
  requisites: { ru: "Реквизиты", en: "Legal details" },
  requisitesHint: {
    ru: "Юрлицо, адрес, регистрационный номер — то, что по закону должно быть на сайте. Показываются в подвале витрины и попадают в разметку организации для поисковиков. Пишите как есть, переводы строк сохраняются.",
    en: "Legal entity, address, registration number — what the law requires on the site. Shown in the storefront footer and put into the organisation markup for search engines. Write it as is, line breaks are kept.",
  },
  requisitesPlaceholder: {
    ru: "ООО «Лавка»\nМинск, ул. Мира, 1\nУНП 123456789",
    en: "Ivan's Shop LLC\n1 Mira St, Minsk\nReg. no. 123456789",
  },
  terms: { ru: "Доставка и оплата", en: "Delivery and payment" },
  termsHint: {
    ru: "Сроки и стоимость доставки, способы оплаты, возврат. Открываются отдельной страницей со ссылкой в подвале витрины. Без них магазин не берут ни Яндекс, ни Google в товарную выдачу. Пустое поле — страницы нет.",
    en: "Delivery times and cost, payment methods, returns. Shown as a separate page linked from the storefront footer. Without them neither Yandex nor Google accepts the shop into shopping results. An empty field means no page.",
  },
  termsPlaceholder: {
    ru: "Доставка курьером по Минску — 1–2 дня, 10 руб.\nПочтой по Беларуси — 3–5 дней.\nОплата наличными или картой при получении.\nВозврат в течение 14 дней.",
    en: "Courier delivery within the city — 1–2 days.\nPost — 3–5 days.\nCash or card on delivery.\nReturns within 14 days.",
  },
  logo: { ru: "Логотип", en: "Logo" },
  logoHint: {
    ru: "Показывается в шапке витрины вместо названия и становится иконкой вкладки. JPEG, PNG, WebP или SVG, до 2 МБ. Без логотипа магазин представлен названием — это тоже нормально.",
    en: "Shown in the storefront header instead of the name, and used as the tab icon. JPEG, PNG, WebP or SVG, up to 2 MB. Without one the shop is represented by its name, which is fine too.",
  },
  logoUpload: { ru: "Загрузить логотип", en: "Upload a logo" },
  logoRemove: { ru: "Убрать", en: "Remove" },
  currency: { ru: "Валюта магазина", en: "Shop currency" },
  currencyRub: { ru: "Российский рубль (₽)", en: "Russian ruble (₽)" },
  currencyByn: { ru: "Белорусский рубль (Br)", en: "Belarusian ruble (Br)" },
  currencyPln: { ru: "Польский злотый (zł)", en: "Polish złoty (zł)" },
  currencyKzt: { ru: "Казахстанский тенге (₸)", en: "Kazakhstani tenge (₸)" },
  currencyHint: {
    ru: "В ней цены видят покупатели и поисковики. Цены товаров при смене валюты не пересчитываются — меняется только подпись.",
    en: "Prices are shown in this currency to customers and search engines. Switching it does not convert product prices — only the label changes.",
  },
  mail: {
    ru: "Почта для уведомлений о заказах",
    en: "Email for order notifications",
  },
  mailHintBefore: {
    ru: "Для Яндекс 360: хост smtp.yandex.ru, порт 465. Нужен ",
    en: "For Яндекс 360: host smtp.yandex.ru, port 465. Use an ",
  },
  mailHintBold: { ru: "пароль приложения", en: "app password" },
  mailHintAfter: {
    ru: " (Яндекс ID → Безопасность → Пароли приложений), а не пароль от почты.",
    en: " (Яндекс ID → Безопасность → Пароли приложений), not your mailbox password.",
  },
  smtpHost: { ru: "SMTP-хост", en: "SMTP host" },
  smtpPort: { ru: "Порт", en: "Port" },
  smtpFrom: { ru: "Отправитель писем", en: "Sender address" },
  smtpFromHint: {
    ru: "Адрес в поле «От кого». Пусто — берётся логин. Заполните, если логин не адрес: сервисы рассылок пускают по API-ключу, а почта Google — по основному ящику, тогда как письмо должно приходить от адреса магазина.",
    en: "The address in the From field. Empty means the login is used. Fill it in when the login is not an address: relays sign in by an API key and Google mail by the main mailbox, while the letter should come from the shop's address.",
  },
  smtpUser: { ru: "Логин (полный email)", en: "Login (full email)" },
  smtpPassword: { ru: "Пароль приложения", en: "App password" },
  smtpPasswordSet: { ru: "сохранён", en: "saved" },
  ai: { ru: "Тексты карточек (AI)", en: "Card texts (AI)" },
  aiHint: {
    ru: "Кнопка «Улучшить» в карточке товара переписывает название и описание из прайса поставщика в человеческий текст. Услуга платная и оплачивается отдельно, ключом AdHunters: сам магазин за неё денег не берёт.",
    en: "The \u201cImprove\u201d button on a product rewrites the supplier\u2019s price-list wording into a readable card. It is a paid service, billed separately through an AdHunters key: the shop itself charges nothing for it.",
  },
  aiKey: { ru: "Ключ AdHunters", en: "AdHunters key" },
  aiKeyHint: {
    ru: "Заведите аккаунт и возьмите ключ на",
    en: "Create an account and get a key at",
  },
  testMail: { ru: "Отправить тестовое письмо", en: "Send a test email" },
  testMailOk: {
    ru: "Письмо отправлено — проверьте почту",
    en: "Email sent — check your inbox",
  },
  testMailFail: {
    ru: "Ошибка: письмо не отправлено, проверьте настройки",
    en: "Error: the email was not sent, check the settings",
  },
  seo: { ru: "Счётчики", en: "Counters" },
  seoHint: {
    ru: "Заведите счётчик в кабинете и вставьте сюда его номер — код появится на витрине сам. Пока поля пустые, на витрине нет ни одного скрипта.",
    en: "Create a counter in the provider's cabinet and paste its id here — the snippet appears on the storefront by itself. While the fields are empty the storefront carries no scripts at all.",
  },
  gaId: {
    ru: "Google Analytics (Measurement ID)",
    en: "Google Analytics (measurement ID)",
  },
  metrikaId: {
    ru: "Яндекс Метрика (номер счётчика)",
    en: "Yandex Metrica (counter number)",
  },
  feeds: { ru: "Товарные фиды", en: "Product feeds" },
  feedsHint: {
    ru: "Готовые выгрузки каталога: вставьте ссылку в кабинет площадки, и она сама будет забирать товары. YML — для Яндекса (Вебмастер → «Товары и предложения», товарные кампании Директа), вторая — для Google Merchant Center. Скрытые товары в фиды не попадают.",
    en: "Ready-made catalogue exports: paste the link into the provider's cabinet and it will fetch the products by itself. YML is for Yandex (Webmaster → products, Direct product campaigns), the other one for Google Merchant Center. Hidden products are excluded.",
  },
  feedYandex: { ru: "Яндекс (YML)", en: "Yandex (YML)" },
  feedGoogle: { ru: "Google Merchant", en: "Google Merchant" },
  copy: { ru: "Копировать", en: "Copy" },
  copied: { ru: "Скопировано", en: "Copied" },
  save: { ru: "Сохранить", en: "Save" },
  saved: { ru: "Сохранено", en: "Saved" },
  passwordSection: { ru: "Смена пароля", en: "Change password" },
  passwordHintBefore: {
    ru: "Забыли пароль? На сервере выполните ",
    en: "Forgot your password? Run ",
  },
  passwordHintAfter: {
    ru: " — команда напечатает новый.",
    en: " on the server — it prints a new one.",
  },
  currentPassword: { ru: "Текущий пароль", en: "Current password" },
  newPassword: {
    ru: "Новый пароль (от 8 символов)",
    en: "New password (8 characters or more)",
  },
  changePassword: { ru: "Изменить пароль", en: "Change password" },
  passwordChanged: { ru: "Пароль изменён", en: "Password changed" },
  passwordFail: {
    ru: "Ошибка: проверьте текущий пароль и длину нового (от 8 символов)",
    en: "Error: check the current password and the length of the new one (8 characters or more)",
  },
  errorPrefix: { ru: "Ошибка", en: "Error" },
};

export default function Profile() {
  const [s, setS] = useState<Settings | null>(null);
  const [smtpPassword, setSmtpPassword] = useState("");
  const [aiKey, setAiKey] = useState("");
  const [msg, setMsg] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [passwordMsg, setPasswordMsg] = useState("");
  const [copied, setCopied] = useState("");
  const t = useT(kText);

  useEffect(() => {
    api.settings().then(setS);
  }, []);
  if (!s) return null;

  const save = async () => {
    const body: Record<string, unknown> = { ...s };
    if (smtpPassword) body.smtp_password = smtpPassword;
    // The masked value the form received back must never be saved as the key,
    // so the field is sent only when the owner typed something new.
    if (aiKey) body.adhunters_api_key = aiKey;
    const saved = await api.updateSettings(body);
    setS(saved);
    setCurrency(saved.currency);
    setSmtpPassword("");
    setAiKey("");
    setMsg(t("saved"));
  };

  const changePassword = async () => {
    setPasswordMsg("");
    try {
      await api.changePassword(currentPassword, newPassword);
      setCurrentPassword("");
      setNewPassword("");
      setPasswordMsg(t("passwordChanged"));
    } catch {
      setPasswordMsg(t("passwordFail"));
    }
  };

  return (
    <div className="form-page">
      <h1 className="text-xl font-bold">{t("title")}</h1>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("shop")}</h2>
          <p className="hint">{t("shopHint")}</p>
        </div>
        <div>
          <label className="label">{t("shopName")}</label>
          <input
            className="field"
            placeholder={t("shopNamePlaceholder")}
            value={s.shop_name}
            onChange={(e) => setS({ ...s, shop_name: e.target.value })}
          />
        </div>
        <div>
          <label className="label">{t("shopPhone")}</label>
          <input
            className="field"
            name="shop-phone"
            autoComplete="off"
            placeholder="+7 999 000-00-00"
            value={s.shop_phone}
            onChange={(e) => setS({ ...s, shop_phone: e.target.value })}
          />
        </div>
        <div>
          <label className="label">{t("telegram")}</label>
          <input
            className="field"
            name="shop-telegram"
            autoComplete="off"
            placeholder="@myshop"
            value={s.telegram}
            onChange={(e) => setS({ ...s, telegram: e.target.value })}
          />
          <p className="hint">{t("messengerHint")}</p>
        </div>
        <div>
          <label className="label">{t("whatsapp")}</label>
          <input
            className="field"
            name="shop-whatsapp"
            autoComplete="off"
            placeholder="+7 999 000-00-00"
            value={s.whatsapp}
            onChange={(e) => setS({ ...s, whatsapp: e.target.value })}
          />
        </div>
        <div>
          <label className="label">{t("requisites")}</label>
          <textarea
            className="field"
            rows={4}
            autoComplete="off"
            placeholder={t("requisitesPlaceholder")}
            value={s.requisites}
            onChange={(e) => setS({ ...s, requisites: e.target.value })}
          />
          <p className="hint mt-1">{t("requisitesHint")}</p>
        </div>
        <div>
          <label className="label">{t("terms")}</label>
          <textarea
            className="field"
            rows={5}
            autoComplete="off"
            placeholder={t("termsPlaceholder")}
            value={s.terms}
            onChange={(e) => setS({ ...s, terms: e.target.value })}
          />
          <p className="hint mt-1">{t("termsHint")}</p>
        </div>
        <div>
          <label className="label">{t("logo")}</label>
          <div className="flex flex-wrap items-center gap-3">
            {s.logo && (
              <img
                src={`/uploads/${s.logo}`}
                alt=""
                className="border-line h-12 w-auto max-w-52 rounded border object-contain"
              />
            )}
            <label className="btn-ghost cursor-pointer">
              {t("logoUpload")}
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp,image/svg+xml"
                className="hidden"
                onChange={async (e) => {
                  const f = e.target.files?.[0];
                  if (f) setS(await api.uploadLogo(f));
                  e.target.value = "";
                }}
              />
            </label>
            {s.logo && (
              <button
                className="text-muted cursor-pointer text-sm hover:text-red-600"
                onClick={async () => setS(await api.deleteLogo())}
              >
                {t("logoRemove")}
              </button>
            )}
          </div>
          <p className="hint mt-1">{t("logoHint")}</p>
        </div>
        <div>
          <label className="label">{t("currency")}</label>
          <select
            className="field"
            value={s.currency}
            onChange={(e) => setS({ ...s, currency: e.target.value })}
          >
            <option value="RUB">{t("currencyRub")}</option>
            <option value="BYN">{t("currencyByn")}</option>
            <option value="PLN">{t("currencyPln")}</option>
            <option value="KZT">{t("currencyKzt")}</option>
          </select>
          <p className="hint mt-1">{t("currencyHint")}</p>
        </div>
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("mail")}</h2>
          <p className="hint">
            {t("mailHintBefore")}
            <b>{t("mailHintBold")}</b>
            {t("mailHintAfter")}
          </p>
        </div>
        <div className="flex gap-3">
          <div className="flex-1">
            <label className="label">{t("smtpHost")}</label>
            <input
              className="field"
              placeholder="smtp.yandex.ru"
              value={s.smtp_host}
              onChange={(e) => setS({ ...s, smtp_host: e.target.value })}
            />
          </div>
          <div className="w-28">
            <label className="label">{t("smtpPort")}</label>
            <input
              className="field"
              type="number"
              value={s.smtp_port}
              onChange={(e) =>
                setS({ ...s, smtp_port: Number(e.target.value) })
              }
            />
          </div>
        </div>
        <div>
          <label className="label">{t("smtpUser")}</label>
          <input
            className="field"
            name="smtp-login"
            autoComplete="off"
            placeholder="shop@example.ru"
            value={s.smtp_user}
            onChange={(e) => setS({ ...s, smtp_user: e.target.value })}
          />
        </div>
        <div>
          <label className="label">{t("smtpFrom")}</label>
          <input
            className="field"
            name="smtp-from"
            autoComplete="off"
            placeholder={s.smtp_user || "shop@example.ru"}
            value={s.smtp_from}
            onChange={(e) => setS({ ...s, smtp_from: e.target.value })}
          />
          <p className="hint mt-1">{t("smtpFromHint")}</p>
        </div>
        <div>
          <label className="label">{t("smtpPassword")}</label>
          <input
            className="field"
            type="password"
            name="smtp-app-password"
            autoComplete="new-password"
            placeholder={s.smtp_password_set ? t("smtpPasswordSet") : ""}
            value={smtpPassword}
            onChange={(e) => setSmtpPassword(e.target.value)}
          />
        </div>
        <div>
          <button
            className="btn-ghost"
            onClick={() =>
              api
                .testSmtp()
                .then(() => setMsg(t("testMailOk")))
                .catch(() => setMsg(t("testMailFail")))
            }
          >
            {t("testMail")}
          </button>
        </div>
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("ai")}</h2>
          <p className="hint">{t("aiHint")}</p>
        </div>
        <div>
          <label className="label">{t("aiKey")}</label>
          <input
            className="field"
            type="password"
            name="adhunters-key"
            autoComplete="new-password"
            placeholder={s.adhunters_api_key || ""}
            value={aiKey}
            onChange={(e) => setAiKey(e.target.value)}
          />
          <p className="hint mt-1">
            {t("aiKeyHint")}{" "}
            <a
              className="text-brand underline"
              href="https://adhunters.fastolead.com"
              target="_blank"
              rel="noopener"
            >
              adhunters.fastolead.com
            </a>
          </p>
        </div>
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("seo")}</h2>
          <p className="hint">{t("seoHint")}</p>
        </div>
        <div className="flex gap-3">
          <div className="flex-1">
            <label className="label">{t("gaId")}</label>
            <input
              className="field"
              autoComplete="off"
              placeholder="G-XXXXXXXXXX"
              value={s.ga_measurement_id}
              onChange={(e) =>
                setS({ ...s, ga_measurement_id: e.target.value })
              }
            />
          </div>
          <div className="flex-1">
            <label className="label">{t("metrikaId")}</label>
            <input
              className="field"
              autoComplete="off"
              placeholder="12345678"
              value={s.metrika_counter_id}
              onChange={(e) =>
                setS({ ...s, metrika_counter_id: e.target.value })
              }
            />
          </div>
        </div>
      </section>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("feeds")}</h2>
          <p className="hint">{t("feedsHint")}</p>
        </div>
        {(
          [
            ["feedYandex", "/yml.xml"],
            ["feedGoogle", "/gmc.xml"],
          ] as const
        ).map(([label, path]) => (
          <div key={path}>
            <label className="label">{t(label)}</label>
            <div className="flex gap-3">
              <input
                className="field flex-1"
                readOnly
                value={location.origin + path}
                onFocus={(e) => e.target.select()}
              />
              <button
                className="btn-ghost"
                onClick={async () => {
                  await navigator.clipboard.writeText(location.origin + path);
                  setCopied(path);
                }}
              >
                {copied === path ? t("copied") : t("copy")}
              </button>
            </div>
          </div>
        ))}
      </section>

      {/* ponytail: the Kufar/Avito section lands in phase 2 with its adapters */}

      <div className="flex items-center gap-4">
        <button className="btn" onClick={save}>
          {t("save")}
        </button>
        {msg && (
          <span
            className={
              msg.startsWith(t("errorPrefix"))
                ? "text-red-600"
                : "text-green-700"
            }
          >
            {msg}
          </span>
        )}
      </div>

      <section className="card flex flex-col gap-4">
        <div>
          <h2 className="font-bold">{t("passwordSection")}</h2>
          <p className="hint">
            {t("passwordHintBefore")}
            <code>sudo fastoshop -reset-password</code>
            {t("passwordHintAfter")}
          </p>
        </div>
        <div>
          <label className="label">{t("currentPassword")}</label>
          <input
            className="field"
            type="password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={(e) => setCurrentPassword(e.target.value)}
          />
        </div>
        <div>
          <label className="label">{t("newPassword")}</label>
          <input
            className="field"
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
          />
        </div>
        <div className="flex items-center gap-4">
          <button className="btn-ghost" onClick={changePassword}>
            {t("changePassword")}
          </button>
          {passwordMsg && (
            <span
              className={
                passwordMsg.startsWith(t("errorPrefix"))
                  ? "text-red-600"
                  : "text-green-700"
              }
            >
              {passwordMsg}
            </span>
          )}
        </div>
      </section>
    </div>
  );
}
