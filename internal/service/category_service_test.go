package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"katalog-api/internal/model"
)

// fakeRepo adalah repository tiruan buatan sendiri (tanpa library mock).
type fakeRepo struct {
	items        map[string]*model.Category
	productCount map[string]int

	createErr error
	listErr   error
	deleted   []string
	nextID    int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{items: map[string]*model.Category{}, productCount: map[string]int{}}
}

func (f *fakeRepo) seed(id, name string, products int) {
	f.items[id] = &model.Category{ID: id, Name: name, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.productCount[id] = products
}

func (f *fakeRepo) nameTaken(name, exceptID string) bool {
	for id, c := range f.items {
		if id != exceptID && c.Name == name {
			return true
		}
	}
	return false
}

func (f *fakeRepo) Create(_ context.Context, name string, desc *string) (*model.Category, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.nameTaken(name, "") {
		return nil, model.ErrDuplicateName
	}
	f.nextID++
	c := &model.Category{ID: "id-" + string(rune('0'+f.nextID)), Name: name, Description: desc}
	f.items[c.ID] = c
	return c, nil
}

func (f *fakeRepo) List(_ context.Context, _ string, limit, offset int) ([]model.Category, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	var all []model.Category
	for _, c := range f.items {
		all = append(all, *c)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id string) (*model.CategoryDetail, error) {
	c, ok := f.items[id]
	if !ok {
		return nil, model.ErrRecordNotFound
	}
	return &model.CategoryDetail{Category: *c, ProductCount: f.productCount[id]}, nil
}

func (f *fakeRepo) Update(_ context.Context, id, name string, desc *string) (*model.Category, error) {
	c, ok := f.items[id]
	if !ok {
		return nil, model.ErrRecordNotFound
	}
	if f.nameTaken(name, id) {
		return nil, model.ErrDuplicateName
	}
	c.Name, c.Description = name, desc
	return c, nil
}

func (f *fakeRepo) WithTx(_ context.Context, fn func(tx CategoryRepository) error) error {
	return fn(f)
}

func (f *fakeRepo) LockActive(_ context.Context, id string) error {
	if _, ok := f.items[id]; !ok {
		return model.ErrRecordNotFound
	}
	return nil
}

func (f *fakeRepo) CountActiveProducts(_ context.Context, id string) (int, error) {
	return f.productCount[id], nil
}

func (f *fakeRepo) SoftDelete(_ context.Context, id string) error {
	delete(f.items, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func strPtr(s string) *string { return &s }

func TestCreate(t *testing.T) {
	tests := []struct {
		name        string
		req         model.CategoryRequest
		setup       func(*fakeRepo)
		wantErr     error
		wantDetails int
	}{
		{name: "valid dengan deskripsi", req: model.CategoryRequest{Name: strPtr("Elektronik"), Description: strPtr("Perangkat elektronik")}},
		{name: "valid tanpa deskripsi", req: model.CategoryRequest{Name: strPtr("Buku")}},
		{name: "nama tepat 3 karakter", req: model.CategoryRequest{Name: strPtr("abc")}},
		{name: "nama tepat 100 karakter", req: model.CategoryRequest{Name: strPtr(strings.Repeat("a", 100))}},
		{name: "deskripsi tepat 500 karakter", req: model.CategoryRequest{Name: strPtr("Buku"), Description: strPtr(strings.Repeat("d", 500))}},
		{name: "nama tidak dikirim", req: model.CategoryRequest{}, wantErr: ErrValidation, wantDetails: 1},
		{name: "nama hanya spasi", req: model.CategoryRequest{Name: strPtr("    ")}, wantErr: ErrValidation, wantDetails: 1},
		{name: "nama 2 karakter", req: model.CategoryRequest{Name: strPtr("ab")}, wantErr: ErrValidation, wantDetails: 1},
		{name: "nama 101 karakter", req: model.CategoryRequest{Name: strPtr(strings.Repeat("a", 101))}, wantErr: ErrValidation, wantDetails: 1},
		{name: "deskripsi 501 karakter", req: model.CategoryRequest{Name: strPtr("Buku"), Description: strPtr(strings.Repeat("d", 501))}, wantErr: ErrValidation, wantDetails: 1},
		{name: "semua error dikembalikan sekaligus", req: model.CategoryRequest{Name: strPtr("a"), Description: strPtr(strings.Repeat("d", 501))}, wantErr: ErrValidation, wantDetails: 2},
		{
			name:    "nama duplikat",
			req:     model.CategoryRequest{Name: strPtr("Elektronik")},
			setup:   func(f *fakeRepo) { f.seed("x", "Elektronik", 0) },
			wantErr: ErrConflict,
		},
		{
			name:    "error repository tak terduga",
			req:     model.CategoryRequest{Name: strPtr("Elektronik")},
			setup:   func(f *fakeRepo) { f.createErr = errors.New("koneksi putus") },
			wantErr: nil, // diperiksa terpisah di bawah
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			if tc.setup != nil {
				tc.setup(repo)
			}
			got, err := NewCategoryService(repo).Create(context.Background(), tc.req)

			switch {
			case repo.createErr != nil:
				if err == nil || errors.Is(err, ErrValidation) || errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
					t.Fatalf("ingin error tak terduga (diteruskan apa adanya), dapat %v", err)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ingin %v, dapat %v", tc.wantErr, err)
				}
				var svcErr *Error
				if errors.As(err, &svcErr) && tc.wantDetails > 0 && len(svcErr.Details) != tc.wantDetails {
					t.Fatalf("ingin %d detail error, dapat %d: %+v", tc.wantDetails, len(svcErr.Details), svcErr.Details)
				}
			default:
				if err != nil {
					t.Fatalf("tidak boleh error, dapat %v", err)
				}
				if got == nil || got.Name != strings.TrimSpace(*tc.req.Name) {
					t.Fatalf("hasil tidak sesuai: %+v", got)
				}
			}
		})
	}
}

func TestList(t *testing.T) {
	tests := []struct {
		name       string
		seedCount  int
		params     model.ListParams
		wantItems  int
		wantTotal  int
		wantPages  int
		repoErr    error
		wantRepoEr bool
	}{
		{name: "kosong", seedCount: 0, params: model.ListParams{Page: 1, Limit: 10}, wantItems: 0, wantTotal: 0, wantPages: 0},
		{name: "satu halaman", seedCount: 3, params: model.ListParams{Page: 1, Limit: 10}, wantItems: 3, wantTotal: 3, wantPages: 1},
		{name: "halaman terakhir sebagian", seedCount: 25, params: model.ListParams{Page: 3, Limit: 10}, wantItems: 5, wantTotal: 25, wantPages: 3},
		{name: "melewati halaman terakhir", seedCount: 5, params: model.ListParams{Page: 9, Limit: 10}, wantItems: 0, wantTotal: 5, wantPages: 1},
		{name: "kelipatan pas", seedCount: 20, params: model.ListParams{Page: 2, Limit: 10}, wantItems: 10, wantTotal: 20, wantPages: 2},
		{name: "error repository", params: model.ListParams{Page: 1, Limit: 10}, repoErr: errors.New("db down"), wantRepoEr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.listErr = tc.repoErr
			for i := 0; i < tc.seedCount; i++ {
				repo.seed(string(rune('a'+i)), "kategori", 0)
			}
			items, meta, err := NewCategoryService(repo).List(context.Background(), tc.params)
			if tc.wantRepoEr {
				if err == nil {
					t.Fatal("ingin error")
				}
				return
			}
			if err != nil {
				t.Fatalf("tidak boleh error: %v", err)
			}
			if items == nil {
				t.Fatal("items tidak boleh nil agar JSON menjadi []")
			}
			if len(items) != tc.wantItems || meta.TotalItems != tc.wantTotal || meta.TotalPages != tc.wantPages {
				t.Fatalf("dapat items=%d total=%d pages=%d", len(items), meta.TotalItems, meta.TotalPages)
			}
			if meta.Page != tc.params.Page || meta.Limit != tc.params.Limit {
				t.Fatalf("meta page/limit salah: %+v", meta)
			}
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		wantErr      error
		wantProducts int
	}{
		{name: "ada dengan produk aktif", id: "a", wantProducts: 12},
		{name: "tidak ditemukan", id: "zzz", wantErr: ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.seed("a", "Elektronik", 12)
			got, err := NewCategoryService(repo).Get(context.Background(), tc.id)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ingin %v, dapat %v", tc.wantErr, err)
				}
				return
			}
			if err != nil || got.ProductCount != tc.wantProducts {
				t.Fatalf("dapat %+v, err %v", got, err)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		req     model.CategoryRequest
		wantErr error
	}{
		{name: "berhasil", id: "a", req: model.CategoryRequest{Name: strPtr("Gadget"), Description: strPtr("baru")}},
		{name: "nama sama dengan dirinya sendiri boleh", id: "a", req: model.CategoryRequest{Name: strPtr("Elektronik"), Description: strPtr("")}},
		{name: "description tidak dikirim (PUT penuh)", id: "a", req: model.CategoryRequest{Name: strPtr("Gadget")}, wantErr: ErrValidation},
		{name: "nama tidak valid", id: "a", req: model.CategoryRequest{Name: strPtr("x"), Description: strPtr("")}, wantErr: ErrValidation},
		{name: "tidak ditemukan", id: "zzz", req: model.CategoryRequest{Name: strPtr("Gadget"), Description: strPtr("")}, wantErr: ErrNotFound},
		{name: "nama dipakai kategori lain", id: "a", req: model.CategoryRequest{Name: strPtr("Pakaian"), Description: strPtr("")}, wantErr: ErrConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.seed("a", "Elektronik", 0)
			repo.seed("b", "Pakaian", 0)
			got, err := NewCategoryService(repo).Update(context.Background(), tc.id, tc.req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ingin %v, dapat %v", tc.wantErr, err)
				}
				return
			}
			if err != nil || got.Name != *tc.req.Name {
				t.Fatalf("dapat %+v, err %v", got, err)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		wantErr     error
		wantMessage string
		wantDeleted bool
	}{
		{name: "berhasil tanpa produk", id: "kosong", wantDeleted: true},
		{name: "ditolak jika masih ada produk aktif", id: "terpakai", wantErr: ErrConflict, wantMessage: "Kategori masih digunakan oleh 12 produk aktif"},
		{name: "tidak ditemukan", id: "zzz", wantErr: ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.seed("kosong", "Kategori Kosong", 0)
			repo.seed("terpakai", "Elektronik", 12)

			err := NewCategoryService(repo).Delete(context.Background(), tc.id)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ingin %v, dapat %v", tc.wantErr, err)
				}
				if tc.wantMessage != "" && err.Error() != tc.wantMessage {
					t.Fatalf("pesan %q, ingin %q", err.Error(), tc.wantMessage)
				}
				if len(repo.deleted) != 0 {
					t.Fatal("tidak boleh ada yang dihapus")
				}
				return
			}
			if err != nil {
				t.Fatalf("tidak boleh error: %v", err)
			}
			if tc.wantDeleted && (len(repo.deleted) != 1 || repo.deleted[0] != tc.id) {
				t.Fatalf("soft delete tidak terpanggil: %v", repo.deleted)
			}
		})
	}
}
