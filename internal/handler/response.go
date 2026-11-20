// Package handler berisi parsing request, validasi format, dan mapping error ke HTTP.
// Handler tidak boleh berisi query SQL.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"katalog-api/internal/service"
)

type Response struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    any        `json:"data,omitempty"`
	Meta    any        `json:"meta,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code    string               `json:"code"`
	Details []service.FieldError `json:"details,omitempty"`
}

const (
	codeValidation         = "VALIDATION_ERROR"
	codeNotFound           = "NOT_FOUND"
	codeConflict           = "CONFLICT"
	codeInternal           = "INTERNAL_ERROR"
	codeServiceUnavailable = "SERVICE_UNAVAILABLE"
)

func writeJSON(w http.ResponseWriter, status int, body Response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("gagal menulis response", "error", err)
	}
}

func writeSuccess(w http.ResponseWriter, status int, message string, data any) {
	writeJSON(w, status, Response{Success: true, Message: message, Data: data})
}

func writeSuccessMeta(w http.ResponseWriter, status int, message string, data, meta any) {
	writeJSON(w, status, Response{Success: true, Message: message, Data: data, Meta: meta})
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var svcErr *service.Error
	errors.As(err, &svcErr)

	switch {
	case errors.Is(err, service.ErrValidation):
		writeJSON(w, http.StatusBadRequest, Response{
			Message: svcErr.Message,
			Error:   &ErrorBody{Code: codeValidation, Details: svcErr.Details},
		})
	case errors.Is(err, service.ErrNotFound):
		writeJSON(w, http.StatusNotFound, Response{
			Message: svcErr.Message,
			Error:   &ErrorBody{Code: codeNotFound},
		})
	case errors.Is(err, service.ErrConflict):
		writeJSON(w, http.StatusConflict, Response{
			Message: svcErr.Message,
			Error:   &ErrorBody{Code: codeConflict},
		})
	default:
		slog.Error("error tak terduga",
			"method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, Response{
			Message: "Terjadi kesalahan pada server",
			Error:   &ErrorBody{Code: codeInternal},
		})
	}
}

// writeUnavailable dipakai health check saat database tidak terjangkau (503).
func writeUnavailable(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusServiceUnavailable, Response{
		Message: message,
		Error:   &ErrorBody{Code: codeServiceUnavailable},
	})
}
