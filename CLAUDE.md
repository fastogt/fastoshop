# CLAUDE.md — fastoshop

Self-hosted shop for a single sole proprietor: SEO storefront + catalog import from WB/Ozon + request-style orders. Open source (AGPL-3.0), repository `git@github.com:fastogt/fastoshop.git`.

## Stack

Go 1.25 (`src/`, module `github.com/fastogt/fastoshop`), SQLite (mattn/go-sqlite3), chi v5, logrus, gofastogt; React 19 + Vite + Tailwind 4 + axios (`web/`, admin panel). Packaging — nfpm → dpkg, systemd, nginx.

## Architecture

- **Storefront** (`app/storefront`) — server-rendered `html/template`, **zero JavaScript**. SEO is priority #1: JSON-LD `schema.org/Product`, sitemap.xml with lastmod, canonical, OG, human-readable slugs with transliteration. None of this may be broken "for convenience".
- **Admin panel** (`web/`, served under `/admin`) — React SPA, protected by a session, `Disallow: /admin` in robots.
- **API** (`app/handler`) — for the admin panel only, prefix `/api`.
- **Channels are vertical slices, not a shared abstraction.** Marketplace sync is built as separate admin tabs (Ozon → Wildberries → Kufar/Avito): each has its own Go package (`app/ozon`, `app/wb`), its own tables with the platform prefix (`ozon_settings`, `ozon_links`), its own rules. The core (`products`, `orders`) knows nothing about platforms. A shared interface, if one ever emerges, is derived from finished tabs, not designed ahead of them. After two finished tabs, exactly one thing turned out to be shared — the markup-ladder arithmetic (`database/price_rules.go`); everything else diverged: Ozon sets stock by `offer_id` and answers per line item immediately, WB sets stock by the size barcode and price by `nmID`, and returns the result as a task later. An interface designed from Ozon alone would have broken exactly there.
- **Import** (`app/importer`) — one-time catalog fill from a single source: Ozon/WB Seller API or a YML export by URL (Bitrix/InSales/Tilda). A pure conversion into our model: platform identifiers are not stored during import; links are established later on the channel tab. Photos are not downloaded during import — the source URL is stored (`product_images.path` accepts both a local name and an absolute URL), otherwise 20,000 cards would mean 60,000 synchronous downloads. Bringing them in-house is a separate background action, "Fill in", in the product table: it replaces `path` rather than inserting new rows, because position determines the main photo.
- **Scale**: proven on 20,000 products — the catalog is paginated (60 items), the admin panel goes up to 500 rows per page with bulk actions, the storefront keeps a page at ~30 KB. SQLite has headroom; a deep OFFSET at ~55 ms is a deliberate ceiling.
- **Single-tenant**: one VPS = one shop = one owner. There is no multi-tenancy and none is planned.

## Mandatory rules

- **HTTP responses only through gofastogt**: the `{"data": ...}` envelope and errors are assembled in one place — `app/httpjson` (`WriteOK` → `gofastogt.NewOkResponse`, `WriteBadRequest`/`WriteInternalError` → `errorgt.MakeErrorJson*`); handler packages keep local aliases `writeOK`/`writeBadRequest` to them. No `json.Marshal` + `w.Write` in handlers.
- **Payloads are named structs**, never `map[string]any` / `interface{}`. An endpoint's response is its own struct next to the handler (`listProductsResponse`, `settingsResponse`). Request bodies to external APIs are structs too.
- **Secrets never leave the server**: passwords and tokens in JSON responses are returned as a boolean `*_set` flag, never as the value.
- Errors: `fmt.Errorf("context: %w", err)`.
- Private constants — `k` prefix (`kMaxUploadSize`, `kSessionTTL`).
- Imports: stdlib → external → internal, separated by a blank line.
- **Languages: everything for the developer is in English, everything for the user goes through translations.** Code comments, Go errors (`fmt.Errorf`), logs, the `CHANGELOG`, and tag release notes are written in English only. Texts the seller sees in the admin panel and the buyer sees on the storefront are localized (ru, en) and **never hardcoded in Go or TSX** — in any language. Existing Russian strings move into translations on the first touch of a file; a mass translation as a separate commit is not done.
- **User-facing text is rendered by the server itself, in the owner's language.** The shop is single-tenant, there is one owner, so the shop language (`settings.lang`) is the user's language. Messages live in `app/i18n` under `k` keys; a handler calls `h.msg(i18n.KeyXxx)`. Emails to the owner, which have no client at all, go the same way. Errors that are stored in the DB are written as a key and translated on read (`i18n.TIfKey`); text that came from a platform is not translated — we do not rewrite someone else's error.
- **Frontend types are checked only via `tsc -b`.** The config is reference-based (`files: []` + `references`), so the familiar `tsc --noEmit` silently checks nothing and always succeeds. `npm run build` does check types.
- **A category is a path in a single column `products.category`** (`Текстиль/Спальня/КПБ Евро`), not a separate entity with `parent_id`. A source with a tree (YML, the Ozon taxonomy, the WB subject directory) fills all segments; a source without one fills a single segment and works as before. A tree node also shows its descendants (`category=? OR category LIKE ?||'/%'`), so a parent has a landing page for a broad query. The hierarchy is **not guessed**: only a model could sort flat names into levels, and import must be deterministic and work without keys — that is the job of our onboarding tool, not the shop.
- **One table for the whole admin panel** — `web/src/DataTable.tsx`: row selection, bulk-action bar, page numbers, page size, sorting. Sorting and pagination are always server-side: sorting the loaded page in the browser on 20,000 products is a lie. New lists take this component; we do not write our own table markup.
- **A bulk action accepts either a list of ids or a filter** (`all` + `q` + `supplier`): 20,000 rows cannot be ticked with checkboxes. Deletion is the exception — only by an explicit list.
- **Fields with keys and logins are shielded from autofill** (`autoComplete="off"` / `"new-password"` and a custom `name`). The browser mistakes a "text + password" pair for a login form and fills in the admin password — saving would have put it in the database in plain text.
- **A long job is a background task, not a long request.** A synchronous handler runs into nginx's `proxy_read_timeout` (60 s by default), and this is not a hypothesis: an import of 24,000 products ran right at the edge. Starting a job arms the task and responds immediately; progress reaches the admin panel via `GET /api/job/stream` (SSE) with a mandatory `X-Accel-Buffering: no` — otherwise nginx holds the events until the end of a response that a stream never has. `GET /api/job` remains a snapshot for page load. There is **one** task per instance: there is one owner, they never have parallel long jobs, and two passes over the same rows would fight each other. Every task has cancellation — without it, the only way to stop a 60,000-photo download is restarting the service.
- **Progress is a list of stages, not a single number.** `stages: [{task, done, total, state}]`: a task may have several steps with their own counters, and averaging them into one bar is lying. A new step is added as a row in the task list and a branch in the dispatcher, not as a new endpoint.
- **The CLI and its stdout have an external consumer — the private fastoshop-infra repo.** `fastoshopctl create` calls `fastoshop -invite-owner`, parses the `Invite (valid 24h): <url>` line with `sed`, and emails it — this is the platform's only onboarding path. The flags, `/api/invite`, and the output format look unused from inside this repository, but they must not break: before removing any flag or endpoint, or changing stdout, grep `fastoshop-infra/packaging/`.
- Comments explain **why**, not what. Deliberate simplifications are marked `ponytail:` with the ceiling and the upgrade path stated.

## Versions and releases

The version exists in exactly one place — **the git tag**. It is not in the code: `VersionApp` is injected by the linker (`LDFLAGS` in the Makefile), and the package is built from the tag by the release workflow. Never edit a version number in the sources by hand.

Format — SemVer `vMAJOR.MINOR.PATCH`:

| Increment | When | Examples |
|---|---|---|
| **PATCH** (`v1.0.1`) | A fix with no behavior change for the shop owner | Fixed layout, a wrong calculation, a leak into a log, an external API failure handled |
| **MINOR** (`v1.1.0`) | A new capability, backward compatible | A new channel adapter, a new import source, a new field in the admin panel, a new endpoint |
| **MAJOR** (`v2.0.0`) | The upgrade requires action from the owner | An incompatible change to `/etc/fastoshop.conf`, a removed endpoint or CLI flag, a change to the `items_json` format |

A release is **not** cut for changes that never reach the user: README edits, screenshots, CI, tests, refactoring without a behavior change. A tag on such things is noise in the release list.

**DB schema.** There is no migration framework, and until the first stable release none is needed: the schema is edited directly in the `CREATE TABLE` statements in `database.go`; live databases that would need catching up do not exist yet. A schema change is at minimum **MINOR**.

`ponytail:` after v1, columns are added to `settings` as a list of `ALTER TABLE ADD COLUMN` statements with a duplicate-column check (this is how `addSettingsColumns` lived before the release). As soon as we need to rename a column, change a type, or touch a table with data — a runner on `PRAGMA user_version`: an array of migrations, a fresh database is stamped with the current version immediately, an old one catches up one migration at a time. We do not write it ahead of time.

Cutting a release: append a block to `CHANGELOG` (format as in fastometa: `X.Y.Z / Month D, YYYY`, author in square brackets, list of changes), then `git tag -a vX.Y.Z -F notes.md` and `git push origin vX.Y.Z` — from there the `Release` workflow builds the `.deb`, runs the tests, and attaches the package to the release.

**The tag annotation does not end up in the release description.** The workflow generates the body itself, as a single line with a version-comparison link. After the build, the description must be set explicitly: `gh release edit vX.Y.Z --notes-file notes.md`. We got burned by this twice: a release sat there with no description.

## Commands

```bash
cd src && go fmt ./... && go vet ./... && go test ./...
cd src && golangci-lint run ./...     # v2, config .golangci.yml
cd web && npx tsc -b && npm run lint && npm run build
make build && make package-deb
```

Before committing: `go fmt` (Go), `prettier --write` (web). Commits and push — only at the user's explicit request, without `Co-Authored-By`. `go.sum` and `web/package-lock.json` are not committed (in .gitignore).

## Documents

`README.md` — what the product is and how to install it. `CONTRIBUTING.md` — building, pre-PR checks, how to add your own channel or import source. The spec and the development plan live in `docs/superpowers/` and are deliberately not committed (internal kitchen, not product documentation).
