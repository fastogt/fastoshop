import { useState } from "react";
import { api } from "./api";
import { useT } from "./i18n";

const kText = {
  title: { ru: "Ваш магазин готов", en: "Your shop is ready" },
  intro: {
    ru: "Придумайте пароль для входа в админку. Ссылка одноразовая - после этого она перестанет работать.",
    en: "Choose a password for the admin panel. The link works once - after this it stops working.",
  },
  password: {
    ru: "Пароль (мин. 8 символов)",
    en: "Password (8 characters minimum)",
  },
  submit: { ru: "Войти", en: "Sign in" },
  failed: {
    ru: "Ссылка недействительна или уже использована. Запросите новую.",
    en: "The link is invalid or already used. Ask for a new one.",
  },
  short: {
    ru: "Пароль должен быть не короче 8 символов",
    en: "The password must be at least 8 characters",
  },
};

export default function Invite({
  token,
  onDone,
}: {
  token: string;
  onDone: () => void;
}) {
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const t = useT(kText);

  const submit = () => {
    if (password.length < 8) {
      setErr(t("short"));
      return;
    }
    api
      .invite(token, password)
      .then(onDone)
      .catch(() => setErr(t("failed")));
  };

  return (
    <div className="mx-auto mt-24 flex max-w-sm flex-col gap-3">
      <h1 className="text-xl font-bold">{t("title")}</h1>
      <p className="text-gray-600">{t("intro")}</p>
      <input
        className="field"
        type="password"
        autoComplete="new-password"
        placeholder={t("password")}
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && submit()}
      />
      {err && <p className="text-red-600">{err}</p>}
      <button className="btn" onClick={submit}>
        {t("submit")}
      </button>
    </div>
  );
}
