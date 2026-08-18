import { useState } from "react";
import { api } from "./api";
import { useT } from "./i18n";

const kText = {
  title: { ru: "Вход", en: "Sign in" },
  password: { ru: "Пароль", en: "Password" },
  submit: { ru: "Войти", en: "Sign in" },
  failed: {
    ru: "Неверный email или пароль",
    en: "Wrong email or password",
  },
};

export default function Login({ onDone }: { onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const t = useT(kText);

  const submit = () =>
    api
      .login(email, password)
      .then(onDone)
      .catch(() => setErr(t("failed")));

  return (
    <form
      className="mx-auto mt-24 flex max-w-sm flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <h1 className="text-xl font-bold">{t("title")}</h1>
      <input
        className="field"
        type="email"
        autoComplete="username"
        placeholder="Email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
      />
      <input
        className="field"
        type="password"
        autoComplete="current-password"
        placeholder={t("password")}
        value={password}
        onChange={(e) => setPassword(e.target.value)}
      />
      {err && <p className="text-red-600">{err}</p>}
      <button className="btn" type="submit">
        {t("submit")}
      </button>
    </form>
  );
}
