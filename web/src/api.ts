import axios from "axios";

const http = axios.create({ baseURL: "/api" });

// Shared between the channels: what the shop offers a marketplace and what it
// hears back is the same shape on every platform. The link and error rows are
// not shared - Wildberries hangs stock off a barcode and price off a card, and
// accepts an upload before it is applied.
export interface Candidate {
  product_id: number;
  sku: string;
  title: string;
  stock: number;
  price: number;
  hidden: boolean;
  published: boolean;
}

interface CandidatePage {
  products: Candidate[];
  total: number;
  page: number;
  page_size: number;
}

// reason is only stated by Wildberries: Ozon has one way for a card to be
// missing, WB also has a card with several sizes that one article cannot pick.
export interface UnlinkedProduct {
  id: number;
  title: string;
  sku: string;
  reason?: string;
}

interface PublishResult {
  published: number;
  no_card: UnlinkedProduct[];
}

interface UnpublishResult {
  unpublished: number;
  failed: UnlinkedProduct[];
}

export interface Warehouse {
  id: string;
  name: string;
}

interface WBStockError {
  product_id: number;
  barcode: string;
  stock: number;
  pushed: number;
  error: string;
  retry_at: string | null;
}

interface WBPriceError {
  product_id: number;
  nm_id: number;
  price: number;
  pushed: number;
  error: string;
  retry_at: string | null;
}

export interface WBSettings {
  enabled: boolean;
  token_set: boolean;
  sandbox: boolean;
  warehouse_id: string;
  linked: number;
  unlinked: number;
  pending: number;
  failed: number;
  stock_errors: WBStockError[];
  price_pending: number;
  price_in_flight: number;
  price_failed: number;
  price_errors: WBPriceError[];
  orders_total: number;
  orders_oversold: number;
  orders_unresolved: number;
  poll_error: string;
}

// Money in kopecks, like everywhere else in the admin.
export interface WBLink {
  product_id: number;
  nm_id: number;
  barcode: string;
  vendor_code: string;
  title: string;
  sku: string;
  stock: number;
  shop_price: number;
  price: number;
  stock_pushed: number;
  price_pushed: number;
  in_flight: boolean;
  stock_error: string;
  price_error: string;
}

interface WBLinkPage {
  links: WBLink[];
  total: number;
  page: number;
  page_size: number;
}

export interface WBOrder {
  order_id: number;
  status: string;
  product_id: number | null;
  title: string;
  barcode: string;
  article: string;
  nm_id: number;
  qty: number;
  oversold: boolean;
  created_at: string;
}

interface WBOrderPage {
  orders: WBOrder[];
  total: number;
  page: number;
  page_size: number;
}

// apiError digs the server's message out of the gofastogt error envelope;
// undefined when the failure never reached the server (network, timeout).
export const apiError = (e: unknown): string | undefined =>
  (e as { response?: { data?: { error?: { message?: string } } } }).response
    ?.data?.error?.message;

export interface Product {
  id: number;
  sku: string;
  // What the supplier charged, in minor units: the shelf price is derived from
  // it, so the pricing block can show the whole chain on a real row.
  source_price?: number;
  title: string;
  slug: string;
  description: string;
  price: number;
  stock: number;
  category: string;
  // The maker of the goods, not the supplier who ships them. Merchant Center
  // and schema.org both ask for it, and every source we import states it.
  brand: string;
  supplier: string;
  hidden: boolean;
  // When the row last moved. The admin shows and sorts by it: after an import
  // or a run of rewrites, "what changed" is the only question worth asking of
  // twenty thousand rows.
  updated_at: string;
  // Gross weight in grams and packed size in millimetres. null is "nobody said"
  // and is different from 0: a delivery quote must not read an unweighed
  // product as weightless.
  weight_g: number | null;
  length_mm: number | null;
  width_mm: number | null;
  height_mm: number | null;
  // Characteristics in the order their source stated them. The value keeps the
  // type it came with: a marketplace states a number as a number, and a form
  // states everything as a string.
  params: { name: string; value: string | number | boolean | unknown[] }[];
  images: { id: number; path: string }[];
}

export interface CategoryNode {
  path: string;
  count: number;
  body: string;
  position: number;
  hidden: boolean;
}

// What may change on a category. The move fields are optional on purpose: a
// request that only hides a node must not rename it back to its old name.
interface CategoryPatch {
  name?: string;
  parent?: string;
  position?: number;
  hidden?: boolean;
  body?: string;
}

interface ProductsPage {
  products: Product[];
  total: number;
  page: number;
  pages: number;
}

// A line of the order as it was placed: the price is the one the buyer saw, not
// today's.
interface OrderItem {
  sku: string;
  title: string;
  price: number;
  qty: number;
  // Looked up when the order is read, empty for goods that no longer exist.
  slug: string;
  image: string;
}

export interface Order {
  id: number;
  name: string;
  phone: string;
  email: string;
  comment: string;
  items: OrderItem[];
  total: number;
  // The stored snapshot could not be read: the row needs a human, and its total
  // must not be printed as a zero.
  broken: boolean;
  status: string;
  created_at: string;
}

export interface Settings {
  owner_email: string;
  shop_name: string;
  shop_phone: string;
  // Messenger handles for the "order in one message" buttons on a product page.
  // Empty means no button - the default.
  telegram: string;
  whatsapp: string;
  currency: string;
  lang: string;
  logo: string;
  smtp_host: string;
  smtp_port: number;
  smtp_user: string;
  smtp_from: string;
  smtp_password_set: boolean;
  ga_measurement_id: string;
  metrika_counter_id: string;
  requisites: string;
  terms: string;
  // Last four characters of the stored AdHunters key, empty when none is set.
  // Never the key itself.
  adhunters_api_key: string;
}

interface ImportDiffRow {
  sku: string;
  title: string;
  was: number;
  now: number;
  shelf: number;
  percent: number;
  stock: number;
}

// Sent by GET /job and, live, by the /job/stream event source.
interface JobStage {
  task: string;
  done: number;
  total: number;
  state: "pending" | "running" | "done";
}

export interface JobState {
  running: boolean;
  kind: string;
  stages: JobStage[] | null;
  in_flight: number[] | null;
  stopped: boolean;
  result?: ImportResult;
  error?: string;
}

export interface ImportDiff {
  currency: string;
  total: number;
  new: number;
  gone: number;
  price_up: number;
  price_down: number;
  unchanged: number;
  conflicts: number;
  no_sku: number;
  new_items: ImportDiffRow[];
  gone_items: ImportDiffRow[];
  price_changes: ImportDiffRow[];
}

interface ImportResult {
  imported: number;
  updated: number;
  skipped: number;
  zeroed: number;
  conflicts: number;
  no_sku: number;
  duplicates: number;
  no_price: number;
  errors: number;
}

interface OzonStockError {
  product_id: number;
  offer_id: string;
  stock: number;
  pushed: number;
  error: string;
}

interface OzonPriceError {
  product_id: number;
  offer_id: string;
  price: number;
  pushed: number;
  error: string;
}

export interface OzonSettings {
  enabled: boolean;
  client_id: string;
  api_key_set: boolean;
  warehouse_id: string;
  currency: string;
  linked: number;
  unlinked: number;
  pending: number;
  failed: number;
  stock_errors: OzonStockError[];
  price_pending: number;
  price_failed: number;
  price_errors: OzonPriceError[];
  orders_total: number;
  orders_oversold: number;
  orders_unresolved: number;
  poll_error: string;
}

// All money fields are in kopecks: price is what the owner wants on Ozon,
// shop_price is the shelf price. An empty title means the product is gone and
// only the platform card is left.
export interface OzonLink {
  product_id: number;
  offer_id: string;
  title: string;
  sku: string;
  stock: number;
  shop_price: number;
  price: number;
  stock_pushed: number;
  price_pushed: number;
  stock_error: string;
  price_error: string;
}

export interface OzonLinkPage {
  links: OzonLink[];
  total: number;
  page: number;
  page_size: number;
}

// product_id === null: the item could not be matched to a shop product.
interface OzonOrderItem {
  product_id: number | null;
  offer_id: string;
  title: string;
  qty: number;
}

export interface OzonOrder {
  posting_number: string;
  status: string;
  oversold: boolean;
  created_at: string;
  items: OzonOrderItem[];
}

export interface OzonOrderPage {
  orders: OzonOrder[];
  total: number;
  page: number;
  page_size: number;
}

// What the tab learns when it opens: how the shop's catalogue and the cabinet's
// cards actually overlap. Without it the table lists twenty thousand rows and
// cannot say which of them pressing "Publish" would do anything for.
export interface CabinetState {
  cards: number;
  products: number;
  linked: number;
  ready: number;
  no_card: number;
  orphans: number;
  // Wildberries only: the card exists but carries several sizes, so a vendor
  // code alone cannot pick one. Not a missing card - telling the owner to
  // create one would make a duplicate.
  ambiguous?: number;
  ready_ids: number[];
  // The articles behind `orphans`, capped by the server. The number says
  // whether they matter, the list says which they are.
  orphan_skus: string[];
}

export interface PriceRule {
  up_to: number;
  multiplier: number;
}

const data = <T>(r: { data: { data: T } }) => r.data.data;

export type Stats = {
  server: {
    cpu: number;
    load_average: string;
    memory_total: number;
    memory_free: number;
    hdd_total: number;
    hdd_free: number;
    bandwidth_in: number;
    bandwidth_out: number;
    uptime: number;
    os: { name: string; version: string; arch: string };
    vsystem: string;
    vrole: string;
  };
  shop: {
    products: number;
    products_visible: number;
    orders: number;
    orders_new: number;
    database_bytes: number;
    uploads_bytes: number;
    uploads_files: number;
    process_rss_bytes: number;
    process_cpu: number;
    process_uptime: number;
    version: string;
  };
};

export interface LogInfo {
  available: boolean;
  size: number;
  modified_at: string;
}

// Which slice of the publication table the tab is showing. The states are not
// all answerable in SQL: whether a product has a card is the platform's answer,
// learned once when the tab opened, so "ready" and "no card" travel as the ids
// from that call rather than as a flag.
export type CandidateView = {
  kind: "ready" | "linked" | "nocard" | "all";
  readyIDs: number[];
};

function viewParams(v?: CandidateView) {
  if (!v || v.kind === "all") return {};
  if (v.kind === "linked") return { state: "linked" };
  if (v.kind === "ready") return { ids: v.readyIDs.join(",") };
  return { state: "unlinked", exclude: v.readyIDs.join(",") };
}

export const api = {
  stats: () => http.get("/stats").then(data<Stats>),
  logInfo: () => http.get("/logs/info").then(data<LogInfo>),
  setupNeeded: () => http.get("/setup").then(data<{ needed: boolean }>),
  setup: (email: string, password: string) =>
    http.post("/setup", { email, password }),
  invite: (token: string, password: string) =>
    http.post("/invite", { token, password }),
  login: (email: string, password: string) =>
    http.post("/login", { email, password }),
  products: (
    page = 1,
    q = "",
    supplier?: string,
    per?: number,
    sort?: string,
    dir?: string,
  ) =>
    http
      .get("/products", { params: { page, q, supplier, per, sort, dir } })
      .then(data<ProductsPage>),
  paramVisibility: () =>
    http
      .get("/settings/params")
      .then(data<{ params: { name: string; hidden: boolean }[] }>),
  saveParamVisibility: (hidden: string[]) =>
    http
      .put("/settings/params", { hidden })
      .then(data<{ params: { name: string; hidden: boolean }[] }>),
  bulkStock: (body: Record<string, unknown>) =>
    http.post("/products/bulk/stock", body).then(data<{ updated: number }>),
  bulkVisibility: (body: Record<string, unknown>) =>
    http
      .post("/products/bulk/visibility", body)
      .then(data<{ updated: number }>),
  bulkSupplier: (body: Record<string, unknown>) =>
    http.post("/products/bulk/supplier", body).then(data<{ updated: number }>),
  bulkFillCount: (body: Record<string, unknown>) =>
    http
      .post("/products/bulk/fill/count", body)
      .then(data<{ main: number; total: number }>),
  bulkFill: (body: Record<string, unknown>) =>
    http
      .post("/products/bulk/fill", body)
      .then(data<{ started: boolean; total: number }>),
  job: () => http.get("/job").then(data<JobState>),
  stopJob: () => http.post("/job/stop"),
  bulkDelete: (ids: number[]) =>
    http.post("/products/bulk/delete", { ids }).then(data<{ updated: number }>),
  createProduct: (p: Partial<Product>) =>
    http.post("/products", p).then(data<Product>),
  updateProduct: (id: number, p: Partial<Product>) =>
    http.put(`/products/${id}`, p).then(data<Product>),
  uploadImage: (id: number, f: File) => {
    const fd = new FormData();
    fd.append("file", f);
    return http.post(`/products/${id}/images`, fd).then(data<Product>);
  },
  categories: () =>
    http.get("/products/categories").then(data<{ categories: string[] }>),
  categoryTree: () =>
    http.get("/categories").then(data<{ categories: CategoryNode[] }>),
  createCategory: (parent: string, name: string) =>
    http.post("/categories", { parent, name }).then(data<{ path: string }>),
  updateCategory: (path: string, patch: CategoryPatch) =>
    http.put("/categories", { path, ...patch }).then(data<{ path: string }>),
  deleteCategory: (path: string) =>
    http
      .delete("/categories", { params: { path } })
      .then(data<{ status: string }>),
  setImageOrder: (productId: number, ids: number[]) =>
    http
      .put(`/products/${productId}/images/order`, { ids })
      .then(data<Product>),
  deleteImage: (productId: number, imageId: number) =>
    http.delete(`/products/${productId}/images/${imageId}`).then(data<Product>),
  // Returns a draft only - the product is written by the ordinary save, after
  // the owner has read what the model wrote.
  enrichProduct: (productId: number) =>
    http
      .post(`/products/${productId}/enrich`, {})
      .then(data<{ title: string; description: string; category: string }>),
  orders: (page = 1, per = 50, sort?: string, dir?: string) =>
    http
      .get("/orders", { params: { page, per, sort, dir } })
      .then(data<{ orders: Order[]; total: number; pages: number }>),
  setOrderStatus: (id: number, status: string) =>
    http.put(`/orders/${id}/status`, { status }),
  bulkDeleteOrders: (ids: number[]) =>
    http.post("/orders/bulk/delete", { ids }).then(data<{ deleted: number }>),
  bulkOrderStatus: (ids: number[], status: string) =>
    http.post("/orders/bulk/status", { ids, status }).then(
      data<{
        updated: number;
        failed: { id: number; reason: string }[];
      }>,
    ),
  settings: () => http.get("/settings").then(data<Settings>),
  updateSettings: (s: Record<string, unknown>) =>
    http.put("/settings", s).then(data<Settings>),
  uploadLogo: (f: File) => {
    const fd = new FormData();
    fd.append("file", f);
    return http.post("/settings/logo", fd).then(data<Settings>);
  },
  deleteLogo: () => http.delete("/settings/logo").then(data<Settings>),
  testSmtp: () => http.post("/settings/test-smtp"),
  changePassword: (currentPassword: string, newPassword: string) =>
    http.post("/settings/password", {
      current_password: currentPassword,
      new_password: newPassword,
    }),
  logout: () => http.post("/logout"),
  importSuppliers: () =>
    http.get("/import/suppliers").then(data<{ suppliers: string[] }>),
  importFeed: () =>
    http.get("/import/feed").then(data<{ url: string; supplier: string }>),
  importCheck: (body: Record<string, unknown>) =>
    http.post("/import/check", body).then(data<ImportDiff>),
  recomputePrices: (coefficient: number) =>
    http
      .post("/products/recompute-prices", { coefficient })
      .then(data<{ updated: number }>),
  importRun: (body: Record<string, unknown>) =>
    http.post("/import/run", body).then(data<ImportResult>),
  ozonSettings: () => http.get("/ozon/settings").then(data<OzonSettings>),
  saveOzonSettings: (s: Record<string, unknown>) =>
    http.put("/ozon/settings", s).then(data<OzonSettings>),
  ozonCheck: () =>
    http
      .post("/ozon/check")
      .then(data<{ total: number; legal_name: string; currency: string }>),
  ozonWarehouses: () =>
    http
      .post("/ozon/warehouses")
      .then(data<{ warehouses: Warehouse[] }>)
      .then((r) => r.warehouses),
  ozonPush: () =>
    http.post("/ozon/push").then(data<{ pushed: number; failed: number }>),
  ozonOrders: (page: number) =>
    http.get(`/ozon/orders?page=${page}`).then(data<OzonOrderPage>),
  ozonLinks: (page: number) =>
    http.get(`/ozon/links?page=${page}`).then(data<OzonLinkPage>),
  ozonCandidates: (page: number, q: string, view?: CandidateView) =>
    http
      .get(`/ozon/candidates`, { params: { page, q, ...viewParams(view) } })
      .then(data<CandidatePage>),
  ozonCabinet: () => http.get(`/ozon/cabinet`).then(data<CabinetState>),
  ozonPublish: (ids: number[]) =>
    http.post("/ozon/publish", { product_ids: ids }).then(data<PublishResult>),
  ozonUnpublish: (ids: number[]) =>
    http
      .post("/ozon/unpublish", { product_ids: ids })
      .then(data<UnpublishResult>),
  ozonSetPrice: (productId: number, price: number) =>
    http.put(`/ozon/price/${productId}`, { price }),
  priceRules: () =>
    http
      .get("/products/price-rules")
      .then(data<{ rules: PriceRule[]; coefficient: number }>),
  setPriceRules: (rules: PriceRule[]) =>
    http
      .put("/products/price-rules", { rules })
      .then(data<{ rules: PriceRule[] }>),
  ozonPriceRules: () =>
    http.get("/ozon/price/rules").then(data<{ rules: PriceRule[] }>),
  ozonSetPriceRules: (rules: PriceRule[]) =>
    http.put("/ozon/price/rules", { rules }).then(data<{ rules: PriceRule[] }>),
  ozonFillByRules: () =>
    http.post("/ozon/price/fill-by-rules").then(data<{ filled: number }>),
  ozonFillPrices: (markupBp: number) =>
    http
      .post("/ozon/price/fill", { markup_bp: markupBp })
      .then(data<{ filled: number }>),
  wbSettings: () => http.get("/wb/settings").then(data<WBSettings>),
  saveWBSettings: (s: Record<string, unknown>) =>
    http.put("/wb/settings", s).then(data<WBSettings>),
  wbCheck: () =>
    http.post("/wb/check").then(
      data<{
        total: number;
        legal_name: string;
        trade_mark: string;
        no_stock_scope: boolean;
      }>,
    ),
  wbWarehouses: () =>
    http
      .post("/wb/warehouses")
      .then(data<{ warehouses: Warehouse[] }>)
      .then((r) => r.warehouses),
  wbPush: () =>
    http.post("/wb/push").then(data<{ pushed: number; failed: number }>),
  wbOrders: (page: number) =>
    http.get(`/wb/orders?page=${page}`).then(data<WBOrderPage>),
  wbLinks: (page: number) =>
    http.get(`/wb/links?page=${page}`).then(data<WBLinkPage>),
  wbCabinet: () => http.get(`/wb/cabinet`).then(data<CabinetState>),
  wbCandidates: (page: number, q: string, view?: CandidateView) =>
    http
      .get(`/wb/candidates`, { params: { page, q, ...viewParams(view) } })
      .then(data<CandidatePage>),
  wbPublish: (ids: number[]) =>
    http.post("/wb/publish", { product_ids: ids }).then(data<PublishResult>),
  wbUnpublish: (ids: number[]) =>
    http
      .post("/wb/unpublish", { product_ids: ids })
      .then(data<UnpublishResult>),
  wbSetPrice: (productId: number, price: number) =>
    http.put(`/wb/price/${productId}`, { price }),
  wbPriceRules: () =>
    http.get("/wb/price/rules").then(data<{ rules: PriceRule[] }>),
  wbSetPriceRules: (rules: PriceRule[]) =>
    http.put("/wb/price/rules", { rules }).then(data<{ rules: PriceRule[] }>),
  wbFillByRules: () =>
    http.post("/wb/price/fill-by-rules").then(data<{ filled: number }>),
  wbFillPrices: (markupBp: number) =>
    http
      .post("/wb/price/fill", { markup_bp: markupBp })
      .then(data<{ filled: number }>),
};
