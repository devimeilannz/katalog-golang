package handler

import (
	"context"
	"log/slog"
	"net/http"
)

// Pinger dipenuhi oleh repository untuk memeriksa koneksi database.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler melayani GET /health.
type HealthHandler struct {
	db Pinger
}

// NewHealthHandler membuat health handler.
func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

// Check mengembalikan 200 jika database terjangkau, 503 SERVICE_UNAVAILABLE jika tidak.
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		notFound(w, r)
		return
	}
	if err := h.db.Ping(r.Context()); err != nil {
		slog.Error("health check gagal", "error", err)
		writeUnavailable(w, "Database tidak dapat dihubungi")
		return
	}
	writeSuccess(w, http.StatusOK, "Layanan sehat", map[string]string{"status": "ok"})
}
