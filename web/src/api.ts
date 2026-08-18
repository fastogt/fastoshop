import axios from "axios";

const http = axios.create({ baseURL: "/api" });

// Wildberries. Types are not shared with Ozon: the row shapes differ — stock
// hangs off a barcode, price off a card, and an upload is accepted before it is
// applied.
export interface WBStockError {
  product_id: number;
  barcode: string;
  stock: number;
  pushed: number;
  error: string;
  retry_at: string | null;
}

export interface WBPriceError {
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

export interface WBLinkPage {
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

export interface WBOrderPage {
  orders: WBOrder[];
  total: number;
  page: number;
  page_size: number;
}

export interface WBCandidate {
  product_id: number;
  sku: string;
  title: string;
  stock: number;
  price: number;
  hidden: boolean;
  published: boolean;
}

export interface WBCandidatePage {
  products: WBCandidate[];
  total: number;
  page: number;
  page_size: number;
}

export interface WBUnlinkedProduct {
  id: number;
  title: string;
  sku: string;
  reason: string;
}

export interface WBPublishResult {
  published: number;
  no_card: WBUnlinkedProduct[];
}

export interface WBUnpublishResult {
  unpublished: number;
  failed: WBUnlinkedProduct[];
}

export interface WBLinkResult {
  linked: number;
  unlinked_products: WBUnlinkedProduct[];
  unlinked_cards: { nm_id: number; vendor_code: string }[];
}

export interface WBWarehouse {
  id: string;
  name: string;
}

// apiError digs the server's message out of the gofastogt error envelope;
// undefined when the failure never reached the server (network, timeout).
export const apiError = (e: unknown): string | undefined =>
  (e as { response?: { data?: { error?: { message?: string } } } }).response
    ?.data?.error?.message;

export interface Product {
  id: number;
  sku: string;
  title: string;
  slug: string;
  description: string;
  price: number;
  stock: number;
  category: string;
  supplier: string;
  hidden: boolean;
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
export interface CategoryPatch {
  name?: string;
  parent?: string;
  position?: number;
  hidden?: boolean;
  body?: string;
}

export interface ProductsPage {
  products: Product[];
  total: number;
  page: number;
  pages: number;
}

export interface Order {
  id: number;
  name: string;
  phone: string;
  email: string;
  comment: string;
  items_json: string;
  status: string;
  created_at: string;
}

export interface Settings {
  owner_email: string;
  shop_name: string;
  shop_phone: string;
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
}

export interface ImportDiffRow {
  sku: string;
  title: string;
  was: number;
  now: number;
  shelf: number;
  percent: number;
  stock: number;
}

// Sent by GET /job and, live, by the /job/stream event source.
export interface JobStage {
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

export interface ImportResult {
  imported: number;
  updated: number;
  skipped: number;
  zeroed: number;
  // Photos whose source the supplier removed: the link was deleted, so the
  // storefront shows its own placeholder instead of a broken picture.
  dropped: number;
  conflicts: number;
  no_sku: number;
  duplicates: number;
  no_price: number;
  errors: number;
}

export interface OzonStockError {
  product_id: number;
  offer_id: string;
  stock: number;
  pushed: number;
  error: string;
}

export interface OzonPriceError {
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
export interface OzonOrderItem {
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

export interface OzonCandidate {
  product_id: number;
  sku: string;
  title: string;
  stock: number;
  price: number;
  hidden: boolean;
  published: boolean;
}

export interface OzonCandidatePage {
  products: OzonCandidate[];
  total: number;
  page: number;
  page_size: number;
}

export interface OzonPublishResult {
  published: number;
  no_card: { id: number; title: string; sku: string }[];
}

export interface OzonUnpublishResult {
  unpublished: number;
  failed: { id: number; title: string; sku: string }[];
}

export interface OzonPriceRule {
  up_to: number;
  multiplier: number;
}

export interface OzonWarehouse {
  id: string;
  name: string;
}

export interface OzonLinkResult {
  linked: number;
  unlinked_products: { id: number; title: string; sku: string }[];
  unlinked_offers: string[];
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
  bulkStock: (body: Record<string, unknown>) =>
    http.post("/products/bulk/stock", body).then(data<{ updated: number }>),
  bulkVisibility: (body: Record<string, unknown>) =>
    http
      .post("/products/bulk/visibility", body)
      .then(data<{ updated: number }>),
  bulkSupplier: (body: Record<string, unknown>) =>
    http.post("/products/bulk/supplier", body).then(data<{ updated: number }>),
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
  deleteImage: (productId: number, imageId: number) =>
    http.delete(`/products/${productId}/images/${imageId}`).then(data<Product>),
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
  ozonLink: () => http.post("/ozon/link").then(data<OzonLinkResult>),
  ozonWarehouses: () =>
    http
      .post("/ozon/warehouses")
      .then(data<{ warehouses: OzonWarehouse[] }>)
      .then((r) => r.warehouses),
  ozonPush: () =>
    http.post("/ozon/push").then(data<{ pushed: number; failed: number }>),
  ozonOrders: (page: number) =>
    http.get(`/ozon/orders?page=${page}`).then(data<OzonOrderPage>),
  ozonLinks: (page: number) =>
    http.get(`/ozon/links?page=${page}`).then(data<OzonLinkPage>),
  ozonCandidates: (page: number, q: string) =>
    http
      .get(`/ozon/candidates`, { params: { page, q } })
      .then(data<OzonCandidatePage>),
  ozonPublish: (ids: number[]) =>
    http
      .post("/ozon/publish", { product_ids: ids })
      .then(data<OzonPublishResult>),
  ozonUnpublish: (ids: number[]) =>
    http
      .post("/ozon/unpublish", { product_ids: ids })
      .then(data<OzonUnpublishResult>),
  ozonSetPrice: (productId: number, price: number) =>
    http.put(`/ozon/price/${productId}`, { price }),
  ozonPriceRules: () =>
    http.get("/ozon/price/rules").then(data<{ rules: OzonPriceRule[] }>),
  ozonSetPriceRules: (rules: OzonPriceRule[]) =>
    http
      .put("/ozon/price/rules", { rules })
      .then(data<{ rules: OzonPriceRule[] }>),
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
    http
      .post("/wb/check")
      .then(data<{ total: number; legal_name: string; trade_mark: string }>),
  wbLink: () => http.post("/wb/link").then(data<WBLinkResult>),
  wbWarehouses: () =>
    http
      .post("/wb/warehouses")
      .then(data<{ warehouses: WBWarehouse[] }>)
      .then((r) => r.warehouses),
  wbPush: () =>
    http.post("/wb/push").then(data<{ pushed: number; failed: number }>),
  wbOrders: (page: number) =>
    http.get(`/wb/orders?page=${page}`).then(data<WBOrderPage>),
  wbLinks: (page: number) =>
    http.get(`/wb/links?page=${page}`).then(data<WBLinkPage>),
  wbCandidates: (page: number, q: string) =>
    http
      .get(`/wb/candidates`, { params: { page, q } })
      .then(data<WBCandidatePage>),
  wbPublish: (ids: number[]) =>
    http.post("/wb/publish", { product_ids: ids }).then(data<WBPublishResult>),
  wbUnpublish: (ids: number[]) =>
    http
      .post("/wb/unpublish", { product_ids: ids })
      .then(data<WBUnpublishResult>),
  wbSetPrice: (productId: number, price: number) =>
    http.put(`/wb/price/${productId}`, { price }),
  wbPriceRules: () =>
    http.get("/wb/price/rules").then(data<{ rules: OzonPriceRule[] }>),
  wbSetPriceRules: (rules: OzonPriceRule[]) =>
    http
      .put("/wb/price/rules", { rules })
      .then(data<{ rules: OzonPriceRule[] }>),
  wbFillByRules: () =>
    http.post("/wb/price/fill-by-rules").then(data<{ filled: number }>),
  wbFillPrices: (markupBp: number) =>
    http
      .post("/wb/price/fill", { markup_bp: markupBp })
      .then(data<{ filled: number }>),
};
