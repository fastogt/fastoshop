import { api, type JobState } from "./api";
import { useT, type Phrase } from "./i18n";

// One line per task, because a job is a list of steps: today a fill downloads
// photos, tomorrow it may also search, translate or fill in descriptions, and
// averaging those into one bar would say nothing about any of them.
const kText = {
  fetch: { ru: "Читаем выгрузку", en: "Reading the feed" },
  products: { ru: "Товары", en: "Products" },
  photos: { ru: "Фото", en: "Photos" },
  stop: { ru: "Остановить", en: "Stop" },
  stopping: { ru: "Останавливаем…", en: "Stopping…" },
  waiting: { ru: "ждёт", en: "waiting" },
} satisfies Record<string, Phrase>;

export default function JobBar({ job }: { job: JobState | null }) {
  const t = useT(kText);
  if (!job?.running) return null;
  const stages = job.stages ?? [];

  return (
    <div className="card mb-4 flex items-center gap-4">
      <div className="min-w-0 flex-1 space-y-2">
        {stages.map((s) => {
          const percent =
            s.total > 0 ? Math.round((s.done / s.total) * 100) : 0;
          const name =
            s.task in kText ? t(s.task as keyof typeof kText) : s.task;
          return (
            <div
              key={s.task}
              className={s.state === "pending" ? "opacity-50" : ""}
            >
              <div className="mb-1 flex items-baseline justify-between gap-3">
                <span className="font-semibold">
                  {s.total > 0
                    ? `${name}: ${s.done.toLocaleString()} / ${s.total.toLocaleString()}`
                    : name}
                </span>
                <span className="hint">
                  {s.state === "pending"
                    ? t("waiting")
                    : s.total > 0
                      ? `${percent}%`
                      : "…"}
                </span>
              </div>
              {/* An unknown total gets no bar rather than a fake one crawling
                  towards a number nobody has measured yet. */}
              {s.total > 0 && (
                <div className="bg-line h-2 w-full overflow-hidden rounded-full">
                  <div
                    className="bg-brand h-full transition-all"
                    style={{ width: `${percent}%` }}
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>
      <button
        className="btn-ghost"
        disabled={job.stopped}
        onClick={() => void api.stopJob()}
      >
        {job.stopped ? t("stopping") : t("stop")}
      </button>
    </div>
  );
}
