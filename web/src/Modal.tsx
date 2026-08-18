import { useEffect, useRef } from "react";

// Modal for editing a row without leaving the table. An inline form above the
// list meant that opening row 49 threw the owner to the top of the page and
// back — with twenty thousand rows that is the difference between editing and
// hunting. Native <dialog>: Escape, focus trapping and the backdrop come from
// the browser instead of hand-rolled listeners.
interface Props {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
}

export default function Modal({ title, onClose, children, footer }: Props) {
  const ref = useRef<HTMLDialogElement>(null);

  // The component only exists while open, so showModal on mount is enough;
  // unmounting removes the element and the backdrop with it.
  useEffect(() => {
    ref.current?.showModal();
  }, []);

  return (
    <dialog
      ref={ref}
      // Escape triggers the dialog's own close; route it to the caller so the
      // state that mounted us is cleared too.
      onClose={onClose}
      onMouseDown={(e) => {
        // Only a click that starts on the backdrop closes the dialog: releasing
        // the mouse outside after selecting text inside must not throw away
        // what was typed. The backdrop reports the dialog element itself as the
        // target; anything inside the card reports the card's children.
        if (e.target === ref.current) onClose();
      }}
      className="m-auto w-full max-w-3xl bg-transparent p-4 backdrop:bg-black/40 sm:p-8"
    >
      {/* Height is capped by the window, only the body scrolls: on a product with a
          long description and photos "Save" otherwise slides below the bottom edge,
          and the owner hunts for the button by scrolling the backdrop. dvh, not vh:
          on a phone vh ignores the address bar, and the dialog's bottom hides under it. */}
      <div className="card flex max-h-[calc(100dvh-2rem)] flex-col sm:max-h-[calc(100dvh-4rem)]">
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
    </dialog>
  );
}
