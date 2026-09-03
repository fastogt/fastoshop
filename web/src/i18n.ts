import { useSyncExternalStore } from "react";

export type Lang = "ru" | "en";

// Each screen owns its own dictionary and passes it to useT: no global key
// namespace to collide in, and a string lives next to the markup that shows it.
export type Phrase = Record<Lang, string>;
export type Dict = Record<string, Phrase>;

const kStorageKey = "fastoshop.lang";

const initial = (): Lang => {
  const saved = localStorage.getItem(kStorageKey);
  if (saved === "ru" || saved === "en") return saved;
  return navigator.language.startsWith("en") ? "en" : "ru";
};

let lang: Lang = initial();
const listeners = new Set<() => void>();

const getLang = (): Lang => lang;

const apply = (next: Lang) => {
  lang = next;
  localStorage.setItem(kStorageKey, next);
  document.documentElement.lang = next;
  listeners.forEach((fn) => fn());
};

// The server renders errors and owner emails itself, so the language has to
// reach it - a purely local switch would leave every message in the old one.
export const setLang = (next: Lang) => {
  apply(next);
  void fetch("/api/settings/lang", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ lang: next }),
  });
};

// adoptLang takes the language stored on the server without pushing it back.
export const adoptLang = (next: string) => {
  if ((next === "ru" || next === "en") && next !== lang) apply(next);
};

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
};

export const useLang = (): Lang =>
  useSyncExternalStore(subscribe, getLang, getLang);

// Placeholders are {name}. Plural forms are deliberately not supported: Russian
// needs three of them, and every phrase here can be written as "Label: N"
// instead of counting words.
export const useT = <T extends Dict>(dict: T) => {
  const current = useLang();
  return (key: keyof T, vars?: Record<string, string | number>): string => {
    let out = dict[key][current];
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        out = out.replaceAll(`{${k}}`, String(v));
      }
    }
    return out;
  };
};
