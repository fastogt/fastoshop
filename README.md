# FastoShop

[![CI](https://github.com/fastogt/fastoshop/actions/workflows/ci.yml/badge.svg)](https://github.com/fastogt/fastoshop/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)

**A second sales channel next to the marketplace: orders from search, no commission, and the buyer's contact stays with you.**

*[Русская версия](README.ru.md) — the original documentation, kept in full.*

FastoShop is a self-hosted online store for a seller who already trades on Wildberries, Ozon, Avito or Kufar. It does not replace the marketplace: everything there stays as it is. What it adds is the channel everybody forgot about while chasing marketplaces — **search**. People still look for goods in Google and Yandex, that channel takes no cut of a sale, and the buyer arrives with a name and a phone number instead of remaining the platform's customer.

It is a seller's back office much like the one on a marketplace — products, orders, stock — except the shop runs on **your domain and your VPS**: no fee for the software, no commission on sales, and no third-party platform that can change the rules overnight.

## What it looks like

| Storefront (what shoppers and search engines see) | Product page |
|---|---|
| ![Catalogue](docs/screenshots/storefront.png) | ![Product](docs/screenshots/product.png) |

| Products in the admin | Catalogue import from Ozon/WB |
|---|---|
| ![Admin](docs/screenshots/admin-products.png) | ![Import](docs/screenshots/admin-import.png) |

## The two things that matter

### The storefront ships no JavaScript

The public side is rendered on the server with `html/template`. We send no framework, no hydration, no bundle: a catalogue page weighs about **16 KB gzipped** and paints immediately. That is not an aesthetic choice, it is mobile ranking.

Out of the box: `schema.org/Product` markup, a sitemap with `lastmod`, canonical URLs, Open Graph, transliterated slugs, and a landing page for every category. Sorting and an "in stock only" filter are plain links — the state lives in the URL and every variant points its canonical at the clean page, so a search engine sees one page instead of five.

The same pages serve AI search. Assistants answer "where do I buy X" from pages they can actually read: server-rendered HTML with the price in markup needs no script execution, and `/llms.txt` — generated from the live catalogue — hands them the whole shop in one read: what is sold, in which sections, how an order works.

Categories arrive with the catalogue: a feed that says `Kitchen > Kettles` becomes a tree. On a live shop one supplier price list produced **570 category pages** instead of a single catalogue.

### The catalogue fills itself

You already have product cards somewhere, and typing them in again is how a shop never launches. Import takes what you have:

- **Ozon or Wildberries seller account** — an API key from the cabinet, and the cards, photos, prices and stock move over.
- **A YML feed** of an existing site (Bitrix, InSales, Tilda) — just the URL.
- **The supplier's own Excel price list.** A real price list is not a tidy table: a logo on top, columns called "Наименование" and "Цена, руб.", half the cells missing, and **photos pasted inside the cells**. Columns are found by their headers, the header row by its content, and pictures come out of the drawing anchors with the rows they belong to. Measured on a live 143 MB file: **23,699 products with 23,647 photos in five seconds**, using only `archive/zip` and `encoding/xml`.
- **A CSV template** for those who would rather fill one in.

Photos are stored as links to the source at import time — 20,000 products would otherwise mean 60,000 synchronous downloads. "Bring the photos in" pulls them onto your own disk later, in the background, with progress and a stop button.

## Marketplace sync

Two-way stock sync with **Ozon** and **Wildberries** over FBS (goods on your own warehouse). Tick the products you also sell on a platform and the stock becomes one number: an order on the storefront reaches the cabinet in seconds, a sale on the platform lowers the shop's stock. The platform price is separate — a flat percentage or a markup ladder, because on a 30-rouble item a percentage does not cover the platform's fee.

Each channel is its own vertical slice (`app/ozon`, `app/wb`) with its own tables, because the platforms only look alike: Ozon sets stock by `offer_id` and answers per item, Wildberries sets it by the size's barcode, prices by card, and reports the result as a task later.

## Everything else

- **Products** — a working table for 20,000 rows: row selection, bulk stock, hiding from the storefront, moving between suppliers, server-side sorting and paging.
- **Cart and orders without online payment** — the buyer leaves a name and a phone or an email, you get a letter, the deal is closed by phone. The cart lives in a cookie; still no JavaScript on the storefront.
- **Legal details and a delivery page** — plain text in the profile becomes the footer of every page, `Organization` markup and a `/info` page. Neither Yandex nor Google admits a shop to shopping results without published terms.
- **Long jobs do not hold the browser** — import and photo downloads run in the background, progress streams to the admin over SSE, one job at a time.
- **Admin in Russian and English**; the storefront speaks the shop's own language.
- **Sales export** — a CSV order journal for the accountant.

## Quick start

```bash
# On a fresh Debian/Ubuntu VPS:
sudo apt install ./fastoshop_<version>_amd64.deb
sudo nano /etc/fastoshop.conf        # base_url: https://shop.example.com
sudo systemctl enable --now fastoshop
# replace DOMAIN_PLACEHOLDER with your domain, then:
sudo cp /usr/share/fastoshop/nginx-fastoshop.conf.template /etc/nginx/sites-enabled/fastoshop
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d shop.example.com
```

Open `https://shop.example.com/admin` and the wizard creates the owner account. On a domain that is already public, create the owner from the console instead and the wizard closes itself:

```bash
sudo fastoshop -create-owner you@example.com   # prints a generated password
sudo fastoshop -reset-password                 # forgot it? no mail server needed
```

Instead of a TCP port the service can listen on a unix socket (`host: "/run/fastoshop.sock"`), and then it is not reachable over the network at all.

## What it does not do

- Online payments and fiscal receipts (planned).
- Product variants: size and colour are separate products for now.
- FBO stock — goods on the platform's own warehouse are counted by the platform.
- Multi-tenancy: one installation is one shop and one owner, deliberately.
- A mobile app. The storefront is responsive; there will be no app.

## Architecture

Go 1.25 with chi and SQLite on the server, React 19 with Vite and Tailwind for the admin. Eight direct dependencies, no framework. Everything ships as one binary plus a database file, which is also the whole backup: copy the file and the uploads directory and the shop moves to another machine.

```
src/app/storefront/   server-rendered storefront, zero JavaScript
src/app/handler/      admin API
src/app/importer/     catalogue import: Ozon, WB, YML, CSV, XLSX
src/app/ozon/         Ozon channel
src/app/wb/           Wildberries channel
src/app/database/     SQLite, one file per shop
web/                  admin SPA
```

## Development

```bash
cd src && go build ./... && go test ./... && golangci-lint run ./...
cd web && npx tsc -b && npm run lint && npm run build
make build && make package-deb
```

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for how to add a channel or an import source.

## License

[AGPL-3.0](LICENSE). Run it yourself and pay nothing. Hosting and support are available at [fastoshop.by](https://fastoshop.by) for those who would rather not administer a server.
