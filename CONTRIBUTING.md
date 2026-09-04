# How to contribute

Thank you for your interest in fastoshop. The project is deliberately small: one Go binary, SQLite, no frameworks beyond the necessary. A PR that adds an abstraction "for the future" will most likely be rejected, while a PR that deletes code will almost certainly be accepted.

## Build and run

You need Go 1.25+ and Node 20+.

```bash
git clone git@github.com:fastogt/fastoshop.git && cd fastoshop

cd src && go test ./... && cd ..          # backend
cd web && npm install && npm run build    # admin panel

# run with a temporary config
printf 'settings:\n  host: "127.0.0.1:9097"\n  database: "/tmp/fastoshop.db"\n  base_url: "http://localhost:9097"\n' > /tmp/fastoshop.conf
cd src && go run ./cmd/fastoshop.go -config /tmp/fastoshop.conf
```

Storefront: http://localhost:9097. Admin panel in dev - `cd web && npm run dev`, then http://localhost:5173/admin/ (`/api` requests are proxied to the backend).

## Before a PR

```bash
cd src && go fmt ./... && go vet ./... && go test ./... && golangci-lint run ./...
cd web && npx prettier --write src/ && npm run lint && npm run build
```

CI runs exactly this. Behavior changes come with a test; bug fixes come with a test that fails before the fix.

## What must not be broken

- **The storefront must not pull in JavaScript.** Public pages are rendered by `html/template`; SEO is the product's main value. Any `<script>` on the storefront, any external CDN or font, is a regression.
- **JSON-LD, canonical, sitemap** are covered by tests in `app/storefront`. If a test gets in the way, it is almost certainly the code that is wrong, not the test.
- **API responses only through `gofastogt`** (the `{"data": ...}` envelope); payloads are named structs, never `map[string]any`.
- **Secrets never go into a response**: passwords and tokens are returned as a `*_set` flag, not as the value.

The full list of conventions is in [CLAUDE.md](CLAUDE.md).

## Adding a marketplace (channel)

Channels are built as vertical slices: an admin tab + an `app/<platform>` package in Go + its own tables with the platform prefix. There is deliberately no shared interface - platform rules differ (Ozon sets stock by `offer_id`, WB by the size barcode, Kufar/Avito have no stock at all). What carries no platform meaning lives in `app/channel` (request parsing, retry delays, response structs, worker signals) and in `web/src/PriceLadder.tsx` / `PublicationPanel.tsx`; a new channel takes those as they are. The reference is the `app/ozon` package; before writing code for a new channel, discuss the design in Issues.

A finished channel brings:

- tables `<platform>_settings`, `<platform>_links`, `<platform>_price_rules`, `<platform>_cursor`, `<platform>_orders` in `database.go`, and the queries for them in `database/<platform>*.go`;
- secrets answered as `*_set` flags, never as values;
- a worker with backoff on `retry_at`, woken through `channel.Signals`;
- an admin tab built on `DataTable` with its own `{ru, en}` dictionary.

The catalogue has no barcode column of its own. A platform that keys stock or price on an EAN keeps it in its link table, the way `wb_links.barcode` does.

## Adding an import source

A one-time catalog transfer, the `Source` interface (`src/app/importer/`):

```go
type Source interface {
    Name() string
    Fetch() ([]Item, error)
}
```

Keep fields like `BaseURL` configurable - mocks in tests point at them. Prices are stored everywhere in minor units (kopecks).

## Versions

The version lives only in the git tag (`vMAJOR.MINOR.PATCH`); it is not in the sources, the number is injected at build time. The rule: **PATCH** - a fix with no behavior change, **MINOR** - a new compatible capability (e.g. an adapter for a new marketplace), **MAJOR** - the upgrade requires action from the shop owner (an incompatible config, a removed endpoint).

Changed the DB schema - that is at minimum MINOR. Until the first stable release the schema is edited directly in `CREATE TABLE` (there are no live databases); after it, the release description needs a ready-to-paste `ALTER TABLE` - there are no migrations, and an upgraded instance will not grow the new column on its own.

No release is cut for edits to documentation, screenshots, CI, or tests.

## Contacts

Questions and bugs - via [Issues](https://github.com/fastogt/fastoshop/issues).
Report vulnerabilities privately: **support@fastocloud.com** - do not open a public issue until the problem is closed.

Code is accepted under the AGPL-3.0 license.
