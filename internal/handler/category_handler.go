package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"katalog-api/internal/model"
	"katalog-api/internal/service"
)

const (
	defaultPage  = 1
	defaultLimit = 10
	maxLimit     = 100
	    maxPage      = 1000000
	maxBodyBytes = 1 << 20 // 1 MB
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CategoryService adalah kontrak yang dibutuhkan handler dari layer service.
type CategoryService interface {
	Create(ctx context.Context, req model.CategoryRequest) (*model.Category, error)
	List(ctx context.Context, p model.ListParams) ([]model.Category, model.Meta, error)
	Get(ctx context.Context, id string) (*model.CategoryDetail, error)
	Update(ctx context.Context, id string, req model.CategoryRequest) (*model.Category, error)
	Delete(ctx context.Context, id string) error
}

// CategoryHandler melayani endpoint /api/v1/categories.
type CategoryHandler struct {
	svc CategoryService
}

// NewCategoryHandler membuat handler. Dependency disuntikkan lewat constructor.
func NewCategoryHandler(svc CategoryService) *CategoryHandler {
	return &CategoryHandler{svc: svc}
}

// Collection melayani /categories (POST dan GET).
func (h *CategoryHandler) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.create(w, r)
	case http.MethodGet:
		h.list(w, r)
	default:
		notFound(w, r)
	}
}

// Item melayani /categories/{id} (GET, PUT, DELETE).
func (h *CategoryHandler) Item(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.get(w, r)
	case http.MethodPut:
		h.update(w, r)
	case http.MethodDelete:
		h.delete(w, r)
	default:
		notFound(w, r)
	}
}

func (h *CategoryHandler) create(w http.ResponseWriter, r *http.Request) {
	req, err := decodeBody(w, r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	c, err := h.svc.Create(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusCreated, "Kategori berhasil dibuat", c.ToResponse())
}

func (h *CategoryHandler) list(w http.ResponseWriter, r *http.Request) {
	params, err := parseListParams(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, meta, err := h.svc.List(r.Context(), params)
	if err != nil {
		writeError(w, r, err)
		return
	}
	data := make([]model.CategoryResponse, 0, len(items))
	for _, c := range items {
		data = append(data, c.ToResponse())
	}
	writeSuccessMeta(w, http.StatusOK, "Daftar kategori berhasil diambil", data, meta)
}

func (h *CategoryHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusOK, "Detail kategori berhasil diambil", d.ToResponse())
}

func (h *CategoryHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	req, err := decodeBody(w, r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	c, err := h.svc.Update(r.Context(), id, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusOK, "Kategori berhasil diperbarui", c.ToResponse())
}

func (h *CategoryHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusOK, "Kategori berhasil dihapus", model.DeletedResponse{ID: id})
}

// parseID memvalidasi format UUID pada path (400 jika bukan UUID).
func parseID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		return "", service.NewValidationError([]service.FieldError{
			{Field: "id", Message: "id harus berupa UUID"},
		})
	}
	return id, nil
}

// parseListParams membaca page, limit, search. Semua pelanggaran dikumpulkan sekaligus.
func parseListParams(r *http.Request) (model.ListParams, error) {
	q := r.URL.Query()
	p := model.ListParams{Page: defaultPage, Limit: defaultLimit, Search: q.Get("search")}
	var details []service.FieldError

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			details = append(details, service.FieldError{Field: "page", Message: "page harus berupa bilangan bulat >= 1"})
		} else {
			p.Page = n
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			details = append(details, service.FieldError{Field: "limit", Message: "limit harus berupa bilangan bulat 1-100"})
		} else {
			p.Limit = n
		}
	}
	if len(details) > 0 {
		return model.ListParams{}, service.NewValidationError(details)
	}
	return p, nil
}

// decodeBody membaca body JSON. JSON rusak atau tipe salah menjadi 400 VALIDATION_ERROR.
func decodeBody(w http.ResponseWriter, r *http.Request) (model.CategoryRequest, error) {
	var req model.CategoryRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)

	if err := dec.Decode(&req); err != nil {
		return req, bodyError(err)
	}
	// Tolak data tambahan setelah objek JSON, misalnya {"a":1}{"b":2}.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return req, service.NewValidationError([]service.FieldError{
			{Field: "body", Message: "body harus berisi tepat satu objek JSON"},
		})
	}
	return req, nil
}

func bodyError(err error) error {
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &typeErr):
		return service.NewValidationError([]service.FieldError{
			{Field: typeErr.Field, Message: typeErr.Field + " harus berupa string"},
		})
	case errors.Is(err, io.EOF):
		return service.NewValidationError([]service.FieldError{
			{Field: "body", Message: "body tidak boleh kosong"},
		})
	default:
		return service.NewValidationError([]service.FieldError{
			{Field: "body", Message: "body bukan JSON yang valid"},
		})
	}
}
