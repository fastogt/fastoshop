# FastoShop

[![CI](https://github.com/fastogt/fastoshop/actions/workflows/ci.yml/badge.svg)](https://github.com/fastogt/fastoshop/actions/workflows/ci.yml)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)

**A second sales channel next to the marketplace: orders from search, no commission, and the buyer's contact stays with you.**

*[Русская версия](README.ru.md) - the original documentation, kept in full.*

FastoShop is your own sales channel next to the marketplace, for a seller who already trades on Wildberries, Ozon, Avito or Kufar. It does not replace the marketplace - everything there stays as it is. It adds the channel everyone forgot while chasing marketplaces: **search**. People still look for goods in Google and Yandex, that channel takes no cut of a sale, and the buyer arrives with a name and a phone number instead of remaining the platform's customer.

The shop runs on **your domain and your server**. No fee for the software, no percentage of your turnover, and no third party that can change the rules overnight.

## What it looks like

| Storefront (what shoppers and search engines see) | Product page |
|---|---|
| ![Catalogue](docs/screenshots/storefront.png) | ![Product](docs/screenshots/product.png) |

| Products in the admin | Catalogue import from Ozon/WB |
|---|---|
| ![Admin](docs/screenshots/admin-products.png) | ![Import](docs/screenshots/admin-import.png) |

## Why a second channel

A marketplace sale is not the price on the tag. Commission, logistics, storage, returns and promotion are all taken from the final price, and on a typical card 15–25% goes on commission alone before anything else is counted. The same order taken on your own site carries one deduction - the payment fee.

That is the arithmetic. Two things matter more than it:

- **The buyer is the platform's, not yours.** You cannot write to a repeat customer, because you never learn who they are. An order on your own site arrives with a name and a phone number.
- **The rules are not yours either.** Promotions, tariffs and category limits change without you. A channel you own keeps working whatever the cabinet decides this quarter.

None of this is an argument for leaving. The marketplace is where buyers find you the first time. The point is that part of your turnover - repeat buyers above all - can arrive without a middleman.

## What you get

**Your catalogue, moved for you.** Cards, photos, prices and stock come from where they already live: an Ozon or Wildberries seller account by API key, a YML feed of an existing site, or the supplier's own Excel price list - the real kind, with a logo on top, missing cells and photos pasted inside the sheet. Measured on a live 143 MB file: **23,699 products with 23,647 photos in five seconds**.

**Pages built to be found.** The storefront sends no JavaScript at all: a catalogue page is about **16 KB gzipped** and paints immediately, which is what mobile ranking is scored on. Product markup, a sitemap with change dates, canonical addresses and human-readable URLs are there from the start, not as a plugin. Categories arrive with the catalogue - one supplier price list produced **570 category pages**, each a landing page for a broad query.

**Ready for paid search on day one.** Two product feeds are generated from the live catalogue and always current: `/yml.xml` for Yandex - a product campaign in Direct or a listing on Market takes the URL and nothing else - and `/gmc.xml` for Google Merchant Center. They carry what those systems require: article, price, currency, availability, category path, picture and description, per offer. On a live shop that is **23,844 offers** with no export step, no plugin and no file to re-upload when a price changes.

**AI assistants can read it too.** They answer "where do I buy X" from pages that need no script to render, and `/llms.txt` - generated from your live catalogue - hands them the whole shop in one read: what is sold, in which sections, how an order works.

**One stock number across channels.** Two-way sync with Ozon and Wildberries for goods on your own warehouse: an order on the site lowers the stock in the cabinet within seconds, a sale on the platform lowers it on the site. The platform price is set separately - a percentage or a markup ladder, because on a 30-rouble item a flat percentage does not cover the platform's fee.

**Cards written for people, not for a warehouse.** A price list carries shorthand like `Ерш унитазный с/подст "Шляпа" д13х36см`. "Improve the text (AI)" turns one card into a name and a description a buyer reads, using only what the source actually says - inventing properties is forbidden. The result is a draft in the form: you read it, edit it and save it yourself, and next week's price refresh never takes it back. Billed separately through an [AdHunters](https://adhunters.fastolead.com) key; without a key the button is not there, and FastoShop charges nothing for it.

**The shop looks like a shop.** Legal details in the footer, a delivery and payment page, a contacts page with your phone and address. Neither Yandex nor Google admits a shop to shopping results without these, and a buyer about to pay a stranger looks for them first.

## How sellers use it alongside the marketplace

The platform brings the first purchase; your own site takes the repeat one. In practice that means putting your address where the buyer already looks - on the packaging, in the card, in correspondence - and letting people who already know what they want come straight to you. They pay the same or less, because the platform's cut is not built into your price, and you keep the contact.

Search adds the second half. Someone typing an exact model with its characteristics is not browsing a marketplace feed; they are in a search box, and that is a query your catalogue page can answer.

## What it does not do

- Online payments and fiscal receipts. An order is a request: the buyer leaves a name and a phone or an email, you get a letter, the deal is closed by phone.
- Promo codes and discount campaigns.
- Product variants: size and colour are separate products for now.
- Customer reviews. Without them a listing carries no stars in search results, and inventing a rating is both forbidden and pointless.
- Delivery and return terms as structured data: both live as free text on the shop's own pages, so search engines cannot read them as fields.
- FBO stock - goods on the platform's own warehouse are counted by the platform.
- Multi-tenancy: one installation is one shop and one owner, deliberately.
- A mobile app. The storefront is responsive; there will be no app.

Search traffic is not instant either. A new site is crawled over weeks, and how much of it is indexed depends on how many pages you have and what is written on them. Nothing here promises a position in the results.

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

After the shop is live, add it to Yandex.Webmaster and Google Search Console and submit the sitemap. In Yandex, set the region to where you actually sell - for commercial queries it decides a great deal.

## Everything else

- **Products** - a working table for 20,000 rows: selection, bulk stock, hiding from the storefront, moving between suppliers, sorting and paging done on the server.
- **Weight and parcel size** - optional and imported from Ozon or Wildberries, where they are mandatory on a card. Unstated stays unstated rather than becoming a zero.
- **Search that matches words** - every word the buyer typed, in any order, in the title or the article.
- **Long jobs do not hold the browser** - import and photo downloads run in the background with progress and a stop button.
- **Admin in Russian and English**; the storefront speaks the shop's own language.
- **Sales export** - a CSV order journal for the accountant.

## Architecture

Go 1.25 with chi and SQLite on the server, React 19 with Vite and Tailwind for the admin. Eight direct dependencies, no framework. Everything ships as one binary plus a database file, which is also the whole backup: take the database and the uploads directory and the shop moves to another machine.

Take the database with `sqlite3 shop.db "VACUUM INTO 'backup.db'"` rather than by copying the file. The shop runs SQLite in WAL mode, so a live database keeps part of its data in a sibling `-wal` file and a plain copy of `shop.db` is short of whatever has not been checkpointed yet.

```
src/app/storefront/   server-rendered storefront, zero JavaScript
src/app/handler/      admin API
src/app/importer/     catalogue import: Ozon, WB, YML, CSV, XLSX
src/app/ozon/         Ozon channel
src/app/wb/           Wildberries channel
src/app/database/     SQLite, one file per shop
web/                  admin SPA
```

Each channel is its own vertical slice with its own tables, because the platforms only look alike: Ozon sets stock by `offer_id` and answers per item, Wildberries sets it by the size's barcode, prices by card, and reports the result as a task later.

## Development

```bash
cd src && go build ./... && go test ./... && golangci-lint run ./...
cd web && npx tsc -b && npm run lint && npm run build
make build && make package-deb
```

Contributions are welcome - see [CONTRIBUTING.md](CONTRIBUTING.md) for how to add a channel or an import source.

## License

[AGPL-3.0](LICENSE). Run it yourself and pay nothing. Hosting and support are available at [fastoshop.by](https://fastoshop.by) for those who would rather not administer a server.
