package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"katalog-api/internal/model"
	"katalog-api/internal/service"
)

// stubService mengembalikan hasil yang sudah ditentukan.
type stubService struct {
	err error
}

func (s stubService) Create(_ context.Context, req model.CategoryRequest) (*model.Category, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &model.Category{ID: "3f6c1b2e-8a4d-4c7e-9b1a-2d5e6f7a8b9c", Name: *req.Name, Description: req.Description,
		CreatedAt: time.Date(2026, 11, 17, 6, 30, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 11, 17, 6, 30, 0, 0, time.UTC)}, nil
}
func (s stubService) List(context.Context, model.ListParams) ([]model.Category, model.Meta, error) {
	return []model.Category{}, model.Meta{Page: 1, Limit: 10}, s.err
}
func (s stubService) Get(context.Context, string) (*model.CategoryDetail, error) { return nil, s.err }
func (s stubService) Update(context.Context, string, model.CategoryRequest) (*model.Category, error) {
	return nil, s.err
}
func (s stubService) Delete(context.Context, string) error { return s.err }

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

const validID = "3f6c1b2e-8a4d-4c7e-9b1a-2d5e6f7a8b9c"

func TestCategoryHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{name: "POST sukses", method: "POST", path: "/api/v1/categories", body: `{"name":"Elektronik"}`, wantStatus: 201},
		{name: "POST JSON rusak", method: "POST", path: "/api/v1/categories", body: `{"name":`, wantStatus: 400, wantCode: "VALIDATION_ERROR"},
		{name: "POST body kosong", method: "POST", path: "/api/v1/categories", body: ``, wantStatus: 400, wantCode: "VALIDATION_ERROR"},
		{name: "POST tipe salah", method: "POST", path: "/api/v1/categories", body: `{"name":123}`, wantStatus: 400, wantCode: "VALIDATION_ERROR"},
		{name: "POST duplikat", method: "POST", path: "/api/v1/categories", body: `{"name":"Elektronik"}`, svcErr: &service.Error{Kind: service.ErrConflict, Message: "Nama kategori sudah digunakan"}, wantStatus: 409, wantCode: "CONFLICT"},
		{name: "POST error tak terduga", method: "POST", path: "/api/v1/categories", body: `{"name":"Elektronik"}`, svcErr: errors.New("boom"), wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "GET list sukses", method: "GET", path: "/api/v1/categories?page=1&limit=10", wantStatus: 200},
		{name: "GET list limit 500", method: "GET", path: "/api/v1/categories?limit=500", wantStatus: 400, wantCode: "VALIDATION_ERROR"},
		{name: "GET list page 0", method: "GET", path: "/api/v1/categories?page=0", wantStatus: 400, wantCode: "VALIDATION_ERROR"},
		{name: "GET id bukan UUID", method: "GET", path: "/api/v1/categories/abc", wantStatus: 400, wantCode: "VALIDATION_ERROR"},
		{name: "GET tidak ditemukan", method: "GET", path: "/api/v1/categories/" + validID, svcErr: service.NewNotFoundError("Kategori tidak ditemukan"), wantStatus: 404, wantCode: "NOT_FOUND"},
		{name: "DELETE sukses", method: "DELETE", path: "/api/v1/categories/" + validID, wantStatus: 200},
		{name: "route tidak dikenal", method: "GET", path: "/api/v1/produk-ajaib", wantStatus: 404, wantCode: "NOT_FOUND"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(
				NewCategoryHandler(stubService{err: tc.svcErr}),
				nil,
				NewHealthHandler(stubPinger{}),
			)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, ingin %d, body %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var resp Response
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("response bukan envelope JSON: %v", err)
			}
			if resp.Success != (tc.wantStatus < 400) {
				t.Fatalf("field success salah: %+v", resp)
			}
			if tc.wantCode != "" && (resp.Error == nil || resp.Error.Code != tc.wantCode) {
				t.Fatalf("kode error %+v, ingin %s", resp.Error, tc.wantCode)
			}
			if tc.wantStatus == 500 && strings.Contains(rec.Body.String(), "boom") {
				t.Fatal("detail teknis tidak boleh bocor ke client")
			}
		})
	}
}

func TestHealth(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantCode   string
	}{
		{name: "database sehat", wantStatus: 200},
		{name: "database mati", pingErr: errors.New("connection refused"), wantStatus: 503, wantCode: "SERVICE_UNAVAILABLE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(
				NewCategoryHandler(stubService{}),
				nil,
				NewHealthHandler(stubPinger{err: tc.pingErr}),
			)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, ingin %d", rec.Code, tc.wantStatus)
			}
			var resp Response
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if tc.wantCode != "" && resp.Error.Code != tc.wantCode {
				t.Fatalf("kode %s", resp.Error.Code)
			}
		})
	}
}
