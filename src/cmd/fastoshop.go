package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/config"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/handler"
	"github.com/fastogt/fastoshop/app/ozon"
	"github.com/fastogt/fastoshop/app/storefront"
	"github.com/fastogt/fastoshop/app/version"
)

func expandHome(p string) string {
	if len(p) >= 2 && p[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// listen: a path starting with a slash is a unix socket, everything else is
// a TCP address. The socket removes the question of handing out ports when
// several independent instances live on one server.
func listen(addr string) (net.Listener, error) {
	if !strings.HasPrefix(addr, "/") {
		return net.Listen("tcp", addr)
	}
	// systemd does not remove the socket after a process killed with SIGKILL —
	// without this the restart would fail with "address already in use".
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stale socket: %w", err)
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	// Set the permissions explicitly: umask makes the socket inaccessible to
	// the group, and nginx connects to it (the unit runs with Group=www-data).
	if err := os.Chmod(addr, 0660); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("socket perms: %w", err)
	}
	return ln, nil
}

func setupLogging(logPath, logLevel string) *os.File {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		level = log.InfoLevel
	}
	log.SetLevel(level)
	log.SetFormatter(&log.TextFormatter{TimestampFormat: "02/01/2006 15:04:05.000", FullTimestamp: true})
	if logPath == "" {
		return nil
	}
	f, err := os.OpenFile(expandHome(logPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Warnf("log file: %v (logging to stderr)", err)
		return nil
	}
	log.SetOutput(f)
	return f
}

func run(cfg *config.Config) error {
	if f := setupLogging(cfg.Settings.LogPath, cfg.Settings.LogLevel); f != nil {
		defer func() { _ = f.Close() }()
	}
	log.Printf("Starting %s %s", version.ProjectName, version.VersionApp)

	dbPath := expandHome(cfg.Settings.Database)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("db dir: %w", err)
	}
	db, err := database.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := db.CleanupExpiredTokens(); err != nil {
		log.Warnf("cleanup expired tokens: %v", err)
	}

	uploadsDir := filepath.Join(filepath.Dir(dbPath), "uploads")
	h := handler.NewHandler(db, uploadsDir)
	sf := storefront.New(db, cfg.Settings.BaseURL, uploadsDir)

	// The stock sync always starts: settings are read on every pass, and
	// enabling pushes from the admin must not require a service restart.
	syncCtx, stopSync := context.WithCancel(context.Background())
	defer stopSync()
	ozonWorker := ozon.NewWorker(db)
	go ozonWorker.Run(syncCtx)
	h.OnStockChange = ozonWorker.StockChanged
	sf.OnStockChange = ozonWorker.StockChanged

	r := chi.NewRouter()
	r.Use(middleware.RealIP) //nolint:staticcheck // behind a trusted nginx reverse proxy
	r.Use(middleware.Compress(5))
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/setup", h.SetupStatus)
		r.Post("/setup", h.Setup)
		r.Post("/invite", h.Invite)
		r.Post("/login", h.Login)
		r.Group(func(r chi.Router) {
			r.Use(h.SessionAuth)
			r.Post("/products", h.CreateProduct)
			r.Get("/products", h.ListProducts)
			r.Put("/products/{id}", h.UpdateProduct)
			r.Delete("/products/{id}", h.DeleteProduct)
			r.Post("/products/{id}/images", h.UploadImage)
			r.Delete("/products/{id}/images/{imageID}", h.DeleteImage)
			r.Post("/products/recompute-prices", h.RecomputePrices)
			r.Get("/products/categories", h.Categories)
			r.Post("/products/bulk/stock", h.BulkStock)
			r.Post("/products/bulk/visibility", h.BulkVisibility)
			r.Post("/products/bulk/supplier", h.BulkSupplier)
			r.Post("/products/bulk/delete", h.BulkDelete)
			r.Post("/products/bulk/fill", h.BulkFill)
			r.Get("/job", h.Job)
			r.Get("/job/stream", h.JobStream)
			r.Post("/job/stop", h.JobStop)
			r.Get("/orders", h.ListOrders)
			r.Put("/orders/{id}/status", h.SetOrderStatus)
			r.Post("/orders/bulk/status", h.BulkOrderStatus)
			r.Get("/orders.csv", h.ExportOrdersCSV)
			r.Get("/stats", h.Stats)
			r.Get("/settings", h.GetSettings)
			r.Put("/settings", h.UpdateSettings)
			r.Put("/settings/lang", h.SetLang)
			r.Post("/settings/logo", h.UploadLogo)
			r.Delete("/settings/logo", h.DeleteLogo)
			r.Post("/settings/password", h.ChangePassword)
			r.Post("/settings/test-smtp", h.TestSMTP)
			r.Post("/logout", h.Logout)
			r.Get("/import/suppliers", h.Suppliers)
			r.Get("/import/template", h.ImportTemplate)
			r.Get("/import/feed", h.Feed)
			r.Post("/import/check", h.ImportCheck)
			r.Post("/import/run", h.ImportRun)
			r.Mount("/ozon", ozon.NewHandlers(db, ozonWorker).Routes())
		})
	})

	// FileServer on its own serves a directory listing: the names of all
	// uploaded files are not something to show publicly.
	uploads := http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir)))
	r.Handle("/uploads/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/") {
			http.NotFound(w, req)
			return
		}
		// The file name carries a random suffix and changes with the content
		// (p<id>-<token>.jpg), so it can be cached forever. Without this
		// header a shopper re-downloads every catalog photo on each page —
		// on a live shop that is megabytes of wasted traffic.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		uploads.ServeHTTP(w, req)
	}))

	// The admin SPA under /admin.
	adminFS := http.Dir(version.ShareFolderPath + "/frontend")
	adminServer := http.StripPrefix("/admin", http.FileServer(adminFS))
	r.Get("/admin*", func(w http.ResponseWriter, req *http.Request) {
		p := req.URL.Path[len("/admin"):]
		if f, err := adminFS.Open(p); err == nil {
			_ = f.Close()
			adminServer.ServeHTTP(w, req)
			return
		}
		req.URL.Path = "/admin/"
		adminServer.ServeHTTP(w, req)
	})

	// Everything else is the storefront.
	r.Mount("/", sf.Router())

	ln, err := listen(cfg.Settings.Host)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Settings.Host, err)
	}

	server := &http.Server{Handler: r}
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	errChan := make(chan error, 1)
	go func() { errChan <- server.Serve(ln) }()

	log.Printf("Server listening on %s", cfg.Settings.Host)
	select {
	case sig := <-sigChan:
		log.Printf("Received %v, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	case err := <-errChan:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

// resetPassword is the recovery path for when the owner forgot the password:
// a self-hosted shop has no support desk, and SMTP is optional and usually
// unconfigured exactly when it is needed most, so recovery lives in the
// binary, not in an email.
func resetPassword(cfg *config.Config) error {
	dbPath := expandHome(cfg.Settings.Database)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("db dir: %w", err)
	}
	db, err := database.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	s, err := db.GetSettings()
	if err != nil {
		return fmt.Errorf("no owner account yet — run -create-owner <email> instead")
	}
	pw, err := db.ResetOwnerPassword()
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	// Print the database and owner before the password: on a server with
	// several instances "wrong config" otherwise goes unnoticed.
	fmt.Printf("Database: %s\nOwner: %s\nNew password: %s\nLog in at /admin with it, then change it under Profile.\n",
		dbPath, s.OwnerEmail, pw)
	return nil
}

// inviteOwner creates the owner and prints a one-time link instead of a
// password: a password would have to be sent by email, where it would keep
// living, while the link burns on first use and expires within a day. The
// shop cannot send emails — SMTP is configured by the owner, who doesn't
// exist yet — so the link goes to stdout and provisioning delivers it
// through its own channel.
func inviteOwner(cfg *config.Config, email string) error {
	dbPath := expandHome(cfg.Settings.Database)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("db dir: %w", err)
	}
	db, err := database.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if _, err := db.CreateOwner(email); err != nil {
		return fmt.Errorf("create owner: %w", err)
	}
	tok, err := db.NewInviteToken()
	if err != nil {
		return fmt.Errorf("invite token: %w", err)
	}
	fmt.Printf("Database: %s\nOwner: %s\nInvite (valid 24h): %s/admin/?invite=%s\n",
		dbPath, email, strings.TrimRight(cfg.Settings.BaseURL, "/"), tok)
	return nil
}

// createOwner creates the owner at provisioning time: until then a fresh
// instance serves an open setup wizard on a public address, and whoever
// opens it first becomes the owner.
func createOwner(cfg *config.Config, email string) error {
	dbPath := expandHome(cfg.Settings.Database)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("db dir: %w", err)
	}
	db, err := database.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	pw, err := db.CreateOwner(email)
	if err != nil {
		return fmt.Errorf("create owner: %w", err)
	}
	fmt.Printf("Database: %s\nOwner: %s\nPassword: %s\nLog in at /admin with it, then change it under Profile.\n",
		dbPath, email, pw)
	return nil
}

func main() {
	ver := flag.Bool("version", false, "display version")
	configPath := flag.String("config", version.ConfigPath, "service config")
	doResetPassword := flag.Bool("reset-password", false, "generate a new owner password and invalidate all sessions")
	ownerEmail := flag.String("create-owner", "", "create the shop owner with a generated password and exit")
	inviteEmail := flag.String("invite-owner", "", "create the shop owner and print a one-time link for them to set their own password")
	flag.Parse()

	if *ver {
		fmt.Printf("%s version %s\n", version.ProjectName, version.VersionApp)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if *inviteEmail != "" {
		if err := inviteOwner(cfg, *inviteEmail); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *ownerEmail != "" {
		if err := createOwner(cfg, *ownerEmail); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *doResetPassword {
		if err := resetPassword(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(cfg); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
