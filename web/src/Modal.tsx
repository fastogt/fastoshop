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
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 sm:p-8"
      onMouseDown={(e) => {
        // Only a click that both starts and ends on the backdrop closes the
        // dialog: releasing the mouse outside after selecting text inside must
        // not throw away what was typed.
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="card w-full max-w-3xl">
        <div className="border-line mb-4 flex items-center justify-between border-b pb-3">
          <h2 className="text-lg font-bold">{title}</h2>
          <button
            className="text-muted hover:text-ink cursor-pointer text-2xl leading-none"
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </div>
        {children}
        {footer && (
          <div className="border-line mt-4 flex items-center gap-3 border-t pt-4">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
