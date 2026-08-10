import { useState } from "react";
import { api } from "./api";
import { useT } from "./i18n";

const kText = {
  title: { ru: "Настройка магазина", en: "Set up your shop" },
  intro: {
    ru: "Создайте аккаунт владельца. Остальное настроите потом в Профиле.",
    en: "Create the owner account. Everything else can be set up later in Profile.",
  },
  password: {
    ru: "Пароль (мин. 8 символов)",
    en: "Password (8 characters minimum)",
  },
  submit: { ru: "Создать", en: "Create" },
  failed: {
    ru: "Проверьте email и пароль (минимум 8 символов)",
    en: "Check the email and the password (8 characters minimum)",
  },
};

export default function Setup({ onDone }: { onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const t = useT(kText);

  return (
    <div className="mx-auto mt-24 flex max-w-sm flex-col gap-3">
      <h1 className="text-xl font-bold">{t("title")}</h1>
      <p className="text-gray-600">{t("intro")}</p>
      <input
        className="field"
        placeholder="Email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
      />
      <input
        className="field"
        type="password"
        autoComplete="new-password"
        placeholder={t("password")}
        value={password}
        onChange={(e) => setPassword(e.target.value)}
      />
      {err && <p className="text-red-600">{err}</p>}
      <button
        className="btn"
        onClick={() =>
          api
            .setup(email, password)
            .then(onDone)
            .catch(() => setErr(t("failed")))
        }
      >
        {t("submit")}
      </button>
    </div>
  );
}
