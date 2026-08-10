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

// listen: путь, начинающийся со слэша, — это unix-сокет, всё остальное —
// TCP-адрес. Сокет снимает вопрос раздачи портов, когда на одном сервере
// живёт несколько независимых инстансов.
func listen(addr string) (net.Listener, error) {
	if !strings.HasPrefix(addr, "/") {
		return net.Listen("tcp", addr)
	}
	// systemd не удаляет сокет за процессом, убитым SIGKILL, — без этого
	// рестарт упал бы на "address already in use".
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stale socket: %w", err)
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	// Права ставим явно: umask делает сокет недоступным для группы, а
	// подключается к нему nginx (юнит запускается с Group=www-data).
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
	h := handler.NewHandler(cfg, db, uploadsDir)
	sf := storefront.New(db, cfg.Settings.BaseURL, uploadsDir)

	// Синк остатков стартует всегда: настройки читаются на каждом проходе, и
	// включение отправки из админки не должно требовать рестарта сервиса.
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

	// FileServer сам по себе отдаёт листинг каталога: имена всех загруженных
	// файлов — не то, что стоит показывать публично.
	uploads := http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir)))
	r.Handle("/uploads/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/") {
			http.NotFound(w, req)
			return
		}
		// Имя файла содержит случайный суффикс и меняется вместе с содержимым
		// (p<id>-<token>.jpg), поэтому кешировать можно навсегда. Без этого
		// заголовка покупатель скачивает все фотографии каталога заново на
		// каждой странице — на живом магазине это мегабайты лишнего трафика.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		uploads.ServeHTTP(w, req)
	}))

	// Админ-SPA под /admin.
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

	// Всё остальное — витрина.
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

// resetPassword — путь восстановления, когда владелец забыл пароль: у
// self-hosted магазина нет службы поддержки, а SMTP опционален и обычно не
// настроен ровно тогда, когда он нужнее всего, поэтому recovery живёт в
// бинаре, а не в письме.
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
	// Печатаем базу и владельца до пароля: на сервере с несколькими инстансами
	// «не тот конфиг» иначе проходит незаметно.
	fmt.Printf("Database: %s\nOwner: %s\nNew password: %s\nLog in at /admin with it, then change it under Profile.\n",
		dbPath, s.OwnerEmail, pw)
	return nil
}

// createOwner заводит владельца в момент провижининга: до этого свежий
// инстанс отдаёт открытый мастер настройки на публичном адресе, и владельцем
// станет тот, кто первым его откроет.
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
	flag.Parse()

	if *ver {
		fmt.Printf("%s version %s\n", version.ProjectName, version.VersionApp)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
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
