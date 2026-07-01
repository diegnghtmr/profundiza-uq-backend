// Command api is the Profundiza UQ backend entrypoint: a modular monolith that
// boots the database, applies migrations, and serves the REST API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/config"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	pg "github.com/uniquindio/profundiza-uq/internal/platform/postgres"
	"github.com/uniquindio/profundiza-uq/internal/platform/ratelimit"
	"github.com/uniquindio/profundiza-uq/migrations"

	cataloghttp "github.com/uniquindio/profundiza-uq/internal/catalog/adapter/http"
	catalogpg "github.com/uniquindio/profundiza-uq/internal/catalog/adapter/postgres"
	catalogapp "github.com/uniquindio/profundiza-uq/internal/catalog/app"

	enrollmenthttp "github.com/uniquindio/profundiza-uq/internal/enrollment/adapter/http"
	enrollmentpg "github.com/uniquindio/profundiza-uq/internal/enrollment/adapter/postgres"
	enrollmentapp "github.com/uniquindio/profundiza-uq/internal/enrollment/app"

	"github.com/uniquindio/profundiza-uq/internal/notification"
	notifemail "github.com/uniquindio/profundiza-uq/internal/notification/adapter/email"
	notifhttp "github.com/uniquindio/profundiza-uq/internal/notification/adapter/http"
	notifpg "github.com/uniquindio/profundiza-uq/internal/notification/adapter/postgres"
	notifapp "github.com/uniquindio/profundiza-uq/internal/notification/app"

	reviewhttp "github.com/uniquindio/profundiza-uq/internal/review/adapter/http"
	reviewpg "github.com/uniquindio/profundiza-uq/internal/review/adapter/postgres"
	reviewapp "github.com/uniquindio/profundiza-uq/internal/review/app"

	reportingfile "github.com/uniquindio/profundiza-uq/internal/reporting/adapter/file"
	reportinghttp "github.com/uniquindio/profundiza-uq/internal/reporting/adapter/http"
	reportingpg "github.com/uniquindio/profundiza-uq/internal/reporting/adapter/postgres"
	reportingapp "github.com/uniquindio/profundiza-uq/internal/reporting/app"

	studenthttp "github.com/uniquindio/profundiza-uq/internal/student/adapter/http"
	studentpg "github.com/uniquindio/profundiza-uq/internal/student/adapter/postgres"
	studentapp "github.com/uniquindio/profundiza-uq/internal/student/app"

	adminuserhttp "github.com/uniquindio/profundiza-uq/internal/adminuser/adapter/http"
	adminuserpg "github.com/uniquindio/profundiza-uq/internal/adminuser/adapter/postgres"
	adminuserapp "github.com/uniquindio/profundiza-uq/internal/adminuser/app"

	settingshttp "github.com/uniquindio/profundiza-uq/internal/settings/adapter/http"
	settingspg "github.com/uniquindio/profundiza-uq/internal/settings/adapter/postgres"
	settingsapp "github.com/uniquindio/profundiza-uq/internal/settings/app"

	audithttp "github.com/uniquindio/profundiza-uq/internal/audit/adapter/http"
	auditpg "github.com/uniquindio/profundiza-uq/internal/audit/adapter/postgres"
	auditapp "github.com/uniquindio/profundiza-uq/internal/audit/app"

	windowhttp "github.com/uniquindio/profundiza-uq/internal/window/adapter/http"
	windowpg "github.com/uniquindio/profundiza-uq/internal/window/adapter/postgres"
	windowapp "github.com/uniquindio/profundiza-uq/internal/window/app"

	identityemail "github.com/uniquindio/profundiza-uq/internal/identity/adapter/email"
	identityhttp "github.com/uniquindio/profundiza-uq/internal/identity/adapter/http"
	identitypg "github.com/uniquindio/profundiza-uq/internal/identity/adapter/postgres"
	identitysystem "github.com/uniquindio/profundiza-uq/internal/identity/adapter/system"
	identityapp "github.com/uniquindio/profundiza-uq/internal/identity/app"

	semesterhttp "github.com/uniquindio/profundiza-uq/internal/semester/adapter/http"
	semesterpg "github.com/uniquindio/profundiza-uq/internal/semester/adapter/postgres"
	semesterapp "github.com/uniquindio/profundiza-uq/internal/semester/app"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := connectWithRetry(connectCtx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pg.RunMigrations(ctx, pool, migrations.FS); err != nil {
		return err
	}
	logger.Info("migrations applied")

	// Wire the semester module (driving adapter -> use case -> driven adapter).
	semesterSvc := semesterapp.NewService(semesterpg.NewRepo(pool))

	// Wire the identity module.
	sessionRepo := identitypg.NewSessionRepo(pool)
	authSvc := identityapp.NewAuthService(
		identityapp.Config{AllowedDomains: cfg.AllowedDomains, OTPTTL: cfg.OTPTTL, SessionTTL: cfg.SessionTTL},
		identitypg.NewChallengeRepo(pool),
		identitypg.NewDirectoryRepo(pool),
		sessionRepo,
		identityemail.NewMailer(cfg.SMTPAddr, cfg.MailFrom, cfg.IsDevelopment(), logger),
		identitysystem.Clock{},
		identitysystem.Codes{},
	)
	// Rate-limit OTP login endpoints per client IP.
	// 20 requests per minute is generous for legitimate use (manual login flow)
	// yet tight enough to deter automated OTP enumeration.
	authRateLimiter := ratelimit.New(20, time.Minute, nil)
	identityHandler := identityhttp.NewHandler(authSvc, cfg.CookieSecure, cfg.SessionTTL, logger, authRateLimiter)

	// Wire the catalog module (student offering browser + admin catalog management).
	catalogHandler := cataloghttp.NewHandler(
		catalogapp.NewService(catalogpg.NewRepo(pool)),
		catalogapp.NewAdminService(catalogpg.NewAdminRepo(pool)),
	)

	// Wire the enrollment module (student submit/cancel/list; concurrency-safe).
	enrollmentRepo := enrollmentpg.NewSubmitRepo(pool)
	enrollmentHandler := enrollmenthttp.NewHandler(enrollmentapp.NewEnrollmentService(enrollmentRepo))

	// Wire the review module (admin queue + decisions).
	reviewHandler := reviewhttp.NewHandler(reviewapp.NewService(reviewpg.NewRepo(pool)))

	// Wire the notification module (in-app inbox + email outbox worker).
	notificationHandler := notifhttp.NewHandler(notifapp.NewService(notifpg.NewRepo(pool)))
	notificationWorker := notification.NewWorker(pool, notifemail.NewSMTPSender(cfg.SMTPAddr, cfg.MailFrom), logger, 5*time.Second)

	// Wire the reporting module (async XLSX/PDF report exports).
	reportingRepo := reportingpg.NewRepo(pool)
	reportingHandler := reportinghttp.NewHandler(reportingapp.NewService(reportingRepo))
	reportingGen := reportingfile.NewGenerator(reportingfile.NewPostgresData(pool), cfg.ReportsOutputDir)
	reportingWorker := reportingapp.NewWorker(reportingRepo, reportingGen, logger, 5*time.Second)

	// Wire the admin-managed modules.
	studentHandler := studenthttp.NewHandler(studentapp.NewService(studentpg.NewRepo(pool)))
	adminUserHandler := adminuserhttp.NewHandler(adminuserapp.NewService(adminuserpg.NewRepo(pool)))
	settingsHandler := settingshttp.NewHandler(settingsapp.NewService(settingspg.NewRepo(pool)))
	auditHandler := audithttp.NewHandler(auditapp.NewService(auditpg.NewRepo(pool)))
	windowHandler := windowhttp.NewHandler(windowapp.NewService(windowpg.NewRepo(pool)))

	r := chi.NewRouter()
	r.Use(httpx.Trace)
	r.Use(httpx.RequestLogger(logger))
	r.Use(httpx.Recoverer(logger))
	// Attach the principal (if any) to every request; guards enforce access.
	r.Use(authn.Middleware(authSvc))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		if err := pool.Ping(req.Context()); err != nil {
			httpx.WriteError(w, req, http.StatusServiceUnavailable, httpx.CodeInternal, "Database unavailable.", nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Mount("/auth", identityHandler.AuthRoutes())

		// Authenticated routes.
		api.Group(func(secured chi.Router) {
			secured.Use(authn.RequireAuth)
			// Enforce CSRF token on all state-changing
			// (POST/PUT/PATCH/DELETE) requests in the authenticated group.
			// GET/HEAD/OPTIONS and the public /auth/* routes are exempt.
			secured.Use(authn.RequireCSRF)
			secured.Get("/me", identityHandler.Me)
			secured.Mount("/semesters", semesterhttp.NewHandler(semesterSvc).Routes())
			secured.Mount("/offerings", catalogHandler.Routes())
			secured.Mount("/enrollment-windows", windowHandler.Routes())
			secured.Mount("/enrollment-requests", enrollmentHandler.Routes())
			secured.Mount("/notifications", notificationHandler.Routes())

			// Admin / superadmin routes.
			secured.Group(func(admin chi.Router) {
				admin.Use(authn.RequireRole(authn.RoleAdmin, authn.RoleSuperAdmin))
				admin.Get("/admin/review-queues", reviewHandler.Queue)
				admin.Post("/admin/enrollment-requests/{requestId}/decisions", reviewHandler.Decide)
				admin.Mount("/reports", reportingHandler.Routes())
				admin.Mount("/students", studentHandler.Routes())
				admin.Mount("/audit-events", auditHandler.Routes())
				admin.Mount("/electives", catalogHandler.ElectiveRoutes())
				admin.Mount("/offering-groups", catalogHandler.GroupRoutes())
			})

			// Superadmin-only routes.
			secured.Group(func(superAdmin chi.Router) {
				superAdmin.Use(authn.RequireRole(authn.RoleSuperAdmin))
				superAdmin.Mount("/admin/users", adminUserHandler.Routes())
				superAdmin.Mount("/admin/global-settings", settingsHandler.Routes())
			})
		})
	})

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: r,
		// ReadHeaderTimeout: protect against slowloris on the initial headers.
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout: bound the total time to read the request, including body.
		ReadTimeout: 30 * time.Second,
		// WriteTimeout: generous to allow report-download responses (XLSX/PDF)
		// to complete; async report generation can take tens of seconds before
		// the streamed file response finishes.
		WriteTimeout: 120 * time.Second,
		// IdleTimeout: limit keep-alive connection lifetime.
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		logger.Info("http server listening", slog.String("addr", cfg.HTTPAddr), slog.String("env", cfg.Env))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", slog.Any("error", err))
			stop()
		}
	}()

	// Background workers; ctx cancellation (SIGINT/SIGTERM) stops them.
	workersDone := make(chan struct{})
	go func() {
		defer close(workersDone)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); notificationWorker.Run(ctx) }()
		go func() { defer wg.Done(); reportingWorker.Run(ctx) }()
		go func() { defer wg.Done(); runSessionCleanup(ctx, sessionRepo, logger) }()
		wg.Wait()
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	err = srv.Shutdown(shutdownCtx)
	<-workersDone // let the workers finish their current batch
	return err
}

// sessionCleanupInterval controls how often expired sessions are purged.
const sessionCleanupInterval = time.Hour

// runSessionCleanup periodically deletes sessions whose expires_at is in the
// past and logs how many rows were removed. It exits when ctx is cancelled.
func runSessionCleanup(ctx context.Context, repo *identitypg.SessionRepo, logger *slog.Logger) {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := repo.DeleteExpired(ctx)
			if err != nil {
				logger.Error("session cleanup error", slog.Any("error", err))
				continue
			}
			logger.Info("session cleanup completed", slog.Int64("deleted", n))
		}
	}
}

// connectWithRetry tries to open the pool until it succeeds or the context
// expires, so the API can start in parallel with the database container.
func connectWithRetry(ctx context.Context, url string, logger *slog.Logger) (*pgxpool.Pool, error) {
	var lastErr error
	for {
		pool, err := pg.Connect(ctx, url)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, errors.Join(lastErr, ctx.Err())
		case <-time.After(time.Second):
			logger.Info("waiting for database", slog.Any("error", err))
		}
	}
}
