import { useEffect, useState } from "react";
import { api, type Product } from "./api";

// Search box over the catalogue; `accept` drops rows the caller cannot take.
export default function ProductPicker({
  onPick,
  placeholder,
  accept = () => true,
}: {
  onPick: (p: Product) => void;
  placeholder: string;
  accept?: (p: Product) => boolean;
}) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [found, setFound] = useState<Product[]>([]);

  useEffect(() => {
    if (!open || !q.trim()) return;
    const id = setTimeout(() => {
      void api.products(1, q, undefined, 10).then((r) => setFound(r.products));
    }, 300);
    return () => clearTimeout(id);
  }, [q, open]);

  const rows = found.filter(accept);
  return (
    <div className="relative">
      <input
        className="field"
        placeholder={placeholder}
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        // Delayed so a click on an option lands before the list disappears.
        onBlur={() => setTimeout(() => setOpen(false), 150)}
      />
      {open && q.trim() && rows.length > 0 && (
        <ul className="border-line absolute z-10 mt-1 max-h-64 w-full overflow-auto rounded border bg-white shadow">
          {rows.map((p) => (
            <li key={p.id}>
              <button
                type="button"
                className="hover:bg-brand-soft flex w-full gap-2 px-2 py-1 text-left text-sm"
                onMouseDown={() => {
                  onPick(p);
                  setQ("");
                  setOpen(false);
                }}
              >
                <span className="flex-1">{p.title}</span>
                <span className="text-muted">{p.sku}</span>
                <span>{p.stock}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
