import { useSyncExternalStore } from "react";
import { api } from "./api";
import { adoptLang } from "./i18n";

// The shop's own money, loaded once for the whole admin. Screens that show a
// price should not each fetch the settings to learn which currency it is in.
let currency = "RUB";
let loaded = false;
const listeners = new Set<() => void>();

const kSigns: Record<string, string> = {
  RUB: "₽",
  BYN: "Br",
  PLN: "zł",
  KZT: "₸",
};

// Must match Settings.Sign() in app/database/settings.go — the same price is
// rendered here and on the storefront.
export const signOf = (code: string) => kSigns[code] ?? code;

const getCurrency = () => currency;

export const setCurrency = (next: string) => {
  if (next === currency) return;
  currency = next;
  listeners.forEach((fn) => fn());
};

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
};

// The admin's language lives on the server too, so one request answers both
// questions.
export const loadShop = () => {
  if (loaded) return;
  loaded = true;
  api.settings().then((s) => {
    adoptLang(s.lang);
    setCurrency(s.currency);
  });
};

const useCurrency = () =>
  useSyncExternalStore(subscribe, getCurrency, getCurrency);

export const useSign = () => signOf(useCurrency());

// product_images.path is either a local file name or an absolute source URL:
// the importer keeps a link to the supplier's photo instead of downloading it.
export const imageURL = (path: string) =>
  path.startsWith("http") ? path : `/uploads/${path}`;

export const isRemoteImage = (path: string) => path.startsWith("http");
