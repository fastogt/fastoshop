import { useEffect } from "react";

// Modal for editing a row without leaving the table. An inline form above the
// list meant that opening row 49 threw the owner to the top of the page and
// back — with twenty thousand rows that is the difference between editing and
// hunting.
interface Props {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
}

export default function Modal({ title, onClose, children, footer }: Props) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    // The page behind must not scroll away under the dialog.
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = overflow;
    };
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-hidden bg-black/40 p-4 sm:p-8"
      onMouseDown={(e) => {
        // Only a click that both starts and ends on the backdrop closes the
        // dialog: releasing the mouse outside after selecting text inside must
        // not throw away what was typed.
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {/* Высота ограничена окном, прокручивается только тело: у товара с длинным
          описанием и фотографиями «Сохранить» иначе уезжает под нижний край, и
          владелец ищет кнопку прокруткой подложки. dvh, а не vh: на телефоне vh
          считается без адресной строки, и низ диалога прячется под неё. */}
      <div className="card flex max-h-[calc(100dvh-2rem)] w-full max-w-3xl flex-col sm:max-h-[calc(100dvh-4rem)]">
        <div className="border-line flex shrink-0 items-center justify-between border-b pb-3">
          <h2 className="text-lg font-bold">{title}</h2>
          <button
            className="text-muted hover:text-ink cursor-pointer text-2xl leading-none"
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </div>
        <div className="-mx-1 flex-1 overflow-y-auto px-1 py-4">{children}</div>
        {footer && (
          <div className="border-line flex shrink-0 items-center gap-3 border-t pt-4">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
