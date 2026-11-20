package handler

import (
	"net/http"

	"katalog-api/internal/service"
)

// NewRouter menyusun semua route dan middleware.
// Semua endpoint kecuali /health berada di bawah prefix /api/v1.
func NewRouter(cat *CategoryHandler, product *ProductHandler, health *HealthHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", health.Check)
	mux.HandleFunc("/api/v1/categories", cat.Collection)
	mux.HandleFunc("/api/v1/categories/{id}", cat.Item)
	// Product endpoints
    mux.HandleFunc("/api/v1/products", product.Collection)
    mux.HandleFunc("/api/v1/products/bulk", product.Bulk)
    mux.HandleFunc("/api/v1/products/{id}", product.Item)
	// Route atau method yang tidak dikenal tetap memakai envelope standar.
	mux.HandleFunc("/", notFound)

	return recoverer(requestLogger(mux))
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, service.NewNotFoundError("Endpoint tidak ditemukan"))
}
