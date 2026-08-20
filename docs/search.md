# Search: what it does today and where it breaks

Written after a live defect. Kept because the next person to touch search will
hit the same wall, and the wall is not obvious from the code.

## What search is today

One substring match, in `productWhere` (`src/app/database/products.go`):

```sql
(ulower(title) LIKE ? ESCAPE '\' OR ulower(sku) LIKE ? ESCAPE '\')
```

The pattern is `%<query>%`, lower-cased in Go. The same function serves the
storefront search box, the admin product table and every channel tab's picker,
which is why a shop needs no second index to be searchable.

`ulower` is ours, registered on the connection in `database.go`. It exists
because **SQLite folds case for ASCII only**. Before it, `LIKE '%кастрюля%'`
never matched "Кастрюля": on a live catalogue the storefront answered a
lower-case query with 8 products out of 529, and the shop looked empty to
anyone typing the way people type. Nothing in the code hinted at it — the
fixtures were all one case and the tests passed. If you touch this comparison,
keep both sides going through `ulower`.

Cost: one Go callback per row per field. Measured at **46 ms over 24 000
products** — the same full scan LIKE always was, only correct.

## Where it breaks

A substring is not a query. Everything below is a real query typed on the live
shop and its real result.

Counts below are from the live 24 000-product catalogue, measured, not guessed.

| Query | Found | Why |
|---|---|---|
| `кастрюля` | 529 | the baseline: the word is in the title as written |
| `кпб евро` | **0** | the title reads "КПБ Евро 4 предмета" — the words are there, that exact substring is not |
| `кастрюля 3 л` | **0** | same: every word, in that order, with that spacing, or nothing |
| `кастрюли` | **9** | plural. Nine titles happen to contain the plural; the other 520 pots are invisible |
| `эмалированая` | **0** | one letter short of "эмалированная", and the catalogue disappears |

Two words in the buyer's own order is the common case, not the corner case, and
it returns nothing.

## What to do about it, in order of what it costs

### 1. Split the query into words (an hour, no schema change)

`AND` a `LIKE` per word instead of one for the whole string:

```sql
(ulower(title) LIKE ? OR ulower(sku) LIKE ?) AND (ulower(title) LIKE ? OR ...)
```

Fixes `кпб евро` and `кастрюля 3 л` — the two that return nothing at all. Does
nothing for plurals or typos, and the
scan gets one pass per word — at 46 ms a pass, three words is still under
150 ms on 24 000 products, but it is worth measuring again before shipping.

This is the honest next step: it is small, it needs no migration, and it covers
the failure people actually hit.

### 2. FTS5 with the `unicode61` tokenizer (a day, plus a migration)

The real answer, and the one that retires `ulower` from search:

- a virtual table over `title`, `sku` and maybe `description`;
- triggers to keep it in step with `products`, or a rebuild after import
  (an import writes 24 000 rows in one pass — triggers on each would be felt);
- word matching, prefix matching (`кастрюл*`), and **ranking**, which the
  storefront has never had: today results come back in whatever order the table
  gives, so a product whose title merely contains the word outranks nothing.

Watch out for:
- `unicode61` folds Unicode case properly, so this is also what makes `ulower`
  unnecessary — but only inside FTS, and the admin filters use plain LIKE too;
- the index doubles the writes on import, which is our longest operation;
- a rebuild on a 24 000-row catalogue is seconds, not minutes — measure it,
  because "rebuild after import" is much simpler than triggers if it holds.

Stemming (`кастрюли` → `кастрюля`) is **not** in FTS5 for Russian. That needs a
snowball tokenizer, which SQLite does not ship. Do not promise it.

### 3. Typos, synonyms, "did you mean"

Out of scope for a shop that runs on one VPS with no search service. If it ever
matters, it is a trigram index over the same FTS table, and it is a separate
decision — not something to bolt onto option 1 or 2.

## Rules that must survive any rewrite

- **Search results stay `noindex,follow`.** They already are. An infinite result
  space in the index eats the crawl budget of a catalogue that has 24 000 real
  pages to spend it on.
- **Sorting and paging stay in SQL.** Sorting the loaded page in the browser on
  20 000 products is a lie the admin used to tell; do not let a new search bring
  it back.
- **The same function serves the storefront and the admin.** Two search paths
  means the owner cannot find the product a buyer just described on the phone.
- **Hidden products never leak.** The storefront reads through its own
  `*Visible*` functions for exactly this reason.

## How to check you did not break it

`src/app/database/search_test.go` holds the case-folding contract. Add to it
rather than beside it: a case there is one line, and every failure this file has
caught so far was invisible in the browser.

Live check that found the original defect, worth repeating after any change:

```bash
# these two must return the same number
curl -s -G https://<shop>/api/products --data-urlencode "q=кастрюля" -d per=1
curl -s -G https://<shop>/api/products --data-urlencode "q=Кастрюля" -d per=1
```