import { apiError } from "./api";
import { useLang, type Phrase } from "./i18n";

// A result and an error share one string state per action, and the prefix is
// what tells them apart: the whole admin colours a message red by it.
const kErrorPrefix: Phrase = { ru: "Ошибка", en: "Error" };

export const useFeedback = (fallback: Phrase) => {
  const lang = useLang();
  const prefix = kErrorPrefix[lang];
  const isError = (m: string) => m.startsWith(prefix);
  return {
    fail: (e: unknown) => `${prefix}: ${apiError(e) ?? fallback[lang]}`,
    isError,
    line: (m: string) =>
      m ? (
        <p className={isError(m) ? "text-red-600" : "text-green-700"}>{m}</p>
      ) : null,
  };
};
