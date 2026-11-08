package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/johnbykov1989/s3vault/internal/app"
	"github.com/johnbykov1989/s3vault/internal/blob"
	"github.com/johnbykov1989/s3vault/internal/jobs"
	"github.com/johnbykov1989/s3vault/internal/metadata"
	"github.com/johnbykov1989/s3vault/internal/observability"
	"github.com/johnbykov1989/s3vault/internal/s3api"

	"go.uber.org/zap"
)

func main() {
	// 0. Логгер (Часть 7: observability)
	logger, err := observability.InitLogger("info")
	if err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	// 1. Инициализируем хранилище (ReplicaStore — две директории)
	store := blob.NewReplicaStore("./data", "./data-replica")

	// 2. Инициализируем метаданные (Postgres)
	var repo metadata.Repository
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		pgRepo, err := metadata.NewPostgresRepo(ctx, dbURL)
		if err != nil {
			log.Fatalf("Failed to connect to DB: %v", err)
		}
		defer pgRepo.Close()
		repo = pgRepo
		logger.Info("connected to Postgres")
	} else {
		logger.Warn("DATABASE_URL not set, running without metadata DB")
	}

	// 3. Инициализируем трейсер (OpenTelemetry)
	otlpEndpoint := os.Getenv("OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4318"
	}
	tracerShutdown, err := observability.InitTracer(context.Background(), "s3vault", otlpEndpoint)
	if err != nil {
		logger.Warn("failed to init tracer, continuing without tracing", zap.Error(err))
	} else {
		logger.Info("tracing initialized", zap.String("otlp_endpoint", otlpEndpoint))
		defer func() {
			if err := tracerShutdown(context.Background()); err != nil {
				logger.Warn("failed to shutdown tracer", zap.Error(err))
			}
		}()
	}

	// 4. Запускаем worker pool (если есть база)
	var workerCancel context.CancelFunc
	if repo != nil {
		workerCtx, cancel := context.WithCancel(context.Background())
		workerCancel = cancel

		worker := jobs.NewWorker(repo, 4)
		go worker.Start(workerCtx)
		logger.Info("started workers", zap.Int("count", 4))
	}

	// 5. Инициализируем S3-хендлер
	s3Handler := s3api.NewObjectHandler(store, repo)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.HealthCheck)
	mux.HandleFunc("/readyz", app.ReadinessCheck)
	mux.Handle("/metrics", observability.MetricsHandler())
	mux.Handle("/", s3Handler)

	handler := observability.MetricsMiddleware(mux)

	port := "9000"
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	// 6. Запускаем сервер в отдельной горутине
	go func() {
		logger.Info("starting S3 Gateway", zap.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	// 7. Ждем сигнал Ctrl+C (Graceful Shutdown)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server")

	// 8. Останавливаем воркеры (отменяем контекст)
	if workerCancel != nil {
		workerCancel()
		logger.Info("workers stopped")
	}

	// 9. Останавливаем сервер (graceful shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := app.ShutdownServer(ctx, srv); err != nil {
		logger.Fatal("server forced to shutdown", zap.Error(err))
	}

	logger.Info("server exited properly")
}
