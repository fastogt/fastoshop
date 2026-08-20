# Channel tabs: make the table say what the owner can actually do

A brief for whoever picks this up. The tabs work; what they show is misleading,
and on a large catalogue it is misleading in a way that wastes the owner's time.

## What the tabs actually do

They **project**, they do not publish. There is no card creation anywhere:
grep the channel packages and you will find `/v3/product/list`,
`/v2/products/stocks`, `/v1/product/import/prices`, `/v3/posting/fbs/list` for
Ozon and `/content/v2/get/cards/list`, `/api/v3/stocks/`, `/api/v2/upload/task`,
`/api/v3/orders` for Wildberries. No `/v3/product/import`, no
`/content/v2/cards/upload`.

"Опубликовать" means: fetch the platform's existing cards, match them to our
products **by the seller's article**, and write the pair into `ozon_links` /
`wb_links` (`app/ozon/publish.go`, `app/wb/publish.go`). From then on stock and
price flow. A product whose article is not on the platform comes back in the
`no_card` list and stays unlinked.

The card itself is created by the seller in the platform's own cabinet. That is
a deliberate boundary — see CLAUDE.md on vertical slices — and it is why the
mandatory-card-fields problem has never bitten us.

## The defect

`Candidates` (both tabs) lists **every product in the shop**:

```go
total, err := h.db.CountProducts(q, database.AnySupplier)
list, err := h.db.ListOzonCandidates(q, kCandidatesPageSize, ...)
```

The row carries `published` — a `LEFT JOIN` on the links table — so the owner
can see what is already linked. What the row cannot say is the thing that
decides everything else: **is there a card on the platform for this article at
all?**

On the live shop that is not a nuance. The catalogue is 24 000 products taken
from a wholesaler's price list; the owner's Ozon cabinet holds a few dozen cards
under their own articles. So the table shows 24 000 rows of which almost none
can be linked. Tick a page of a hundred, press the button, get ninety-nine back
in `no_card`. The tab reads as broken even though every part of it works.

## What to build

The list already knows two of the three facts. Add the third and let the row
say which of four states it is in:

| State | What it means | What the owner does |
|---|---|---|
| **Linked** | in `ozon_links` / `wb_links` | nothing; stock and price already flow |
| **Ready to link** | the article exists on both sides | one button |
| **No card** | our product, nothing on the platform | create it in the cabinet, or accept it never goes there |
| **Card without a product** | on the platform, not in the shop | import it, or ignore |

The platform's article list is already fetched inside `Publish`
(`c.ListProducts()` for Ozon). Moving that call to the tab's own load gives
every row its state, and it costs **one API call per tab open** — cache it for
the session rather than calling it per page of a hundred.

Suggested shape, small enough to keep the existing table:

- `Candidates` takes a `state` filter and defaults to something useful rather
  than to everything. "Ready to link" first: it is the only state with an action
  attached.
- The counts go in the header — "связано 42 · можно связать 7 · нет карточки
  23 801" answers "what am I doing here" before any row is read.
- "Card without a product" needs the platform list minus our articles; it is the
  one state that is not a product row at all, so it may deserve its own small
  section rather than a row in the same table.

## Traps

- **The platform list is not free.** Ozon pages it; a cabinet with thousands of
  cards is several calls. Fetch once per tab open, not per page, and never in a
  loop over products.
- **Matching is by article and nothing else.** No fuzzy matching, no titles:
  the same rule import already follows. Two different goods sharing an article
  is the seller's problem to fix in their cabinet, and quietly linking them
  would be worse than showing "no card".
- **Do not let this become card creation.** The moment the table offers "create
  the card for me", six more mandatory fields appear (`description_category_id`
  and `type_id` for Ozon, `subjectID` plus the subject's own required
  characteristics for WB, VAT, brand, barcode) — and the platform's numeric
  category ids are thrown away at import today (`importer/ozon.go` keeps only
  the text path). That is a separate feature with its own vertical slice.
- **`Unpublish` order matters.** It zeroes the stock on the platform *before*
  dropping the link. A card left behind with the last stock we pushed keeps
  selling goods nobody is counting. Whatever the new table does, it must not
  give the owner a way to drop a link without that step.

## Where the value is

Not in prettier rows. The tab currently cannot answer "why can I not publish
this", and the owner's conclusion is that the integration is broken. Four
honest states turn a wall of 24 000 rows into a short list with a button and a
long list with an explanation.
