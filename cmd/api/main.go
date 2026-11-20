package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq" 

	"katalog-api/internal/config"
	"katalog-api/internal/handler"
	"katalog-api/internal/repository"
	"katalog-api/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("aplikasi berhenti karena error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogger(cfg.LogLevel)

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), cfg.QueryTimeout)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		return errors.Join(errors.New("tidak bisa terhubung ke database"), err)
	}

	// Wiring dependency: repository -> service -> handler (lewat constructor, tanpa global).
	categoryRepo := repository.NewCategoryRepository(db, cfg.QueryTimeout)
	categorySvc := service.NewCategoryService(categoryRepo)
	productRepo := repository.NewProductRepository(db, cfg.QueryTimeout)
    productSvc := service.NewProductService(productRepo)
	router := handler.NewRouter(
		handler.NewCategoryHandler(categorySvc),
		handler.NewProductHandler(productSvc),
		handler.NewHealthHandler(categoryRepo),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown saat menerima SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server berjalan", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("sinyal shutdown diterima, menunggu request yang berjalan selesai")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return errors.Join(errors.New("graceful shutdown gagal"), err)
	}
	slog.Info("server berhenti dengan bersih")
	return nil
}

func setupLogger(level string) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})))
}
