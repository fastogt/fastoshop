import { useEffect, useState } from "react";
import { api } from "./api";
import { useT } from "./i18n";
import Ozon from "./Ozon";
import WB from "./WB";

const kText = {
  wb: { ru: "Wildberries", en: "Wildberries" },
  ozon: { ru: "Ozon", en: "Ozon" },
};

type Platform = "wb" | "ozon";

// Both channels behind one menu word; the connected one opens first, because a
// shop that sells on Ozon only should not land on an empty Wildberries form.
export default function Channels() {
  const t = useT(kText);
  const [platform, setPlatform] = useState<Platform | null>(null);

  useEffect(() => {
    void Promise.all([
      api.wbSettings().catch(() => null),
      api.ozonSettings().catch(() => null),
    ]).then(([wb, ozon]) =>
      setPlatform(!wb?.token_set && ozon?.api_key_set ? "ozon" : "wb"),
    );
  }, []);

  if (!platform) return null;
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-2">
        {(["wb", "ozon"] as const).map((p) => (
          <button
            key={p}
            onClick={() => setPlatform(p)}
            className={
              "rounded-full border px-4 py-1.5 text-sm " +
              (platform === p
                ? "border-brand text-brand font-semibold"
                : "border-line text-muted")
            }
          >
            {t(p)}
          </button>
        ))}
      </div>
      {platform === "wb" ? <WB /> : <Ozon />}
    </div>
  );
}
