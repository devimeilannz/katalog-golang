package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"katalog-api/internal/model"
)

// Batas validasi sesuai spesifikasi.
const (
	nameMinLen        = 3
	nameMaxLen        = 100
	descriptionMaxLen = 500
)

// CategoryRepository adalah kontrak yang dibutuhkan service dari layer repository.
// Interface didefinisikan di sisi pemakai (service) sehingga mudah dibuat mock/fake.
type CategoryRepository interface {
	Create(ctx context.Context, name string, description *string) (*model.Category, error)
	List(ctx context.Context, search string, limit, offset int) ([]model.Category, int, error)
	GetByID(ctx context.Context, id string) (*model.CategoryDetail, error)
	Update(ctx context.Context, id, name string, description *string) (*model.Category, error)

	// WithTx menjalankan fn di dalam satu transaksi database.
	// Commit jika fn mengembalikan nil, rollback jika error.
	WithTx(ctx context.Context, fn func(tx CategoryRepository) error) error
	// LockActive mengunci baris kategori aktif (SELECT ... FOR UPDATE).
	LockActive(ctx context.Context, id string) error
	CountActiveProducts(ctx context.Context, id string) (int, error)
	SoftDelete(ctx context.Context, id string) error
}

// CategoryService berisi aturan bisnis kategori.
type CategoryService struct {
	repo CategoryRepository
}

// NewCategoryService membuat service. Dependency disuntikkan lewat constructor.
func NewCategoryService(repo CategoryRepository) *CategoryService {
	return &CategoryService{repo: repo}
}

// Create membuat kategori baru.
func (s *CategoryService) Create(ctx context.Context, req model.CategoryRequest) (*model.Category, error) {
	if err := validateCategory(req, false); err != nil {
		return nil, err
	}
	c, err := s.repo.Create(ctx, strings.TrimSpace(*req.Name), req.Description)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return c, nil
}

// List mengembalikan daftar kategori aktif beserta meta pagination.
func (s *CategoryService) List(ctx context.Context, p model.ListParams) ([]model.Category, model.Meta, error) {
	offset := (p.Page - 1) * p.Limit
	items, total, err := s.repo.List(ctx, strings.TrimSpace(p.Search), p.Limit, offset)
	if err != nil {
		return nil, model.Meta{}, mapRepoError(err)
	}
	if items == nil {
		items = []model.Category{}
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + p.Limit - 1) / p.Limit
	}
	return items, model.Meta{Page: p.Page, Limit: p.Limit, TotalItems: total, TotalPages: totalPages}, nil
}

// Get mengembalikan detail kategori beserta jumlah produk aktif.
func (s *CategoryService) Get(ctx context.Context, id string) (*model.CategoryDetail, error) {
	d, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return d, nil
}

// Update mengganti seluruh field kategori (PUT, penggantian penuh).
func (s *CategoryService) Update(ctx context.Context, id string, req model.CategoryRequest) (*model.Category, error) {
	if err := validateCategory(req, true); err != nil {
		return nil, err
	}
	c, err := s.repo.Update(ctx, id, strings.TrimSpace(*req.Name), req.Description)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return c, nil
}

// Delete melakukan soft delete. Kategori yang masih punya produk aktif ditolak (409).
// Pengecekan dan penghapusan berjalan dalam satu transaksi dengan row lock
// agar tidak ada produk yang masuk di antara dua langkah tersebut.
func (s *CategoryService) Delete(ctx context.Context, id string) error {
	err := s.repo.WithTx(ctx, func(tx CategoryRepository) error {
		if err := tx.LockActive(ctx, id); err != nil {
			return err
		}
		n, err := tx.CountActiveProducts(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return newConflict(fmt.Sprintf("Kategori masih digunakan oleh %d produk aktif", n))
		}
		return tx.SoftDelete(ctx, id)
	})
	if err != nil {
		return mapRepoError(err)
	}
	return nil
}

// validateCategory menerapkan semua aturan dan mengembalikan seluruh error sekaligus.
// requireDescription true untuk PUT: semua field wajib dikirim.
func validateCategory(req model.CategoryRequest, requireDescription bool) error {
	var details []FieldError

	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		details = append(details, FieldError{Field: "name", Message: "name wajib diisi"})
	} else {
		n := utf8.RuneCountInString(strings.TrimSpace(*req.Name))
		if n < nameMinLen || n > nameMaxLen {
			details = append(details, FieldError{Field: "name", Message: fmt.Sprintf("name harus 3-100 karakter (saat ini %d)", n)})
		}
	}

	if req.Description == nil {
		if requireDescription {
			details = append(details, FieldError{Field: "description", Message: "description wajib dikirim pada PUT"})
		}
	} else if n := utf8.RuneCountInString(*req.Description); n > descriptionMaxLen {
		details = append(details, FieldError{Field: "description", Message: fmt.Sprintf("description maksimal 500 karakter (saat ini %d)", n)})
	}

	if len(details) > 0 {
		return NewValidationError(details)
	}
	return nil
}

// mapRepoError menerjemahkan error repository ke error service.
// Error yang sudah berupa *Error dibiarkan, error lain diteruskan apa adanya
// dan akan menjadi 500 INTERNAL_ERROR di layer handler.
func mapRepoError(err error) error {
	var svcErr *Error
	switch {
	case errors.As(err, &svcErr):
		return err
	case errors.Is(err, model.ErrRecordNotFound):
		return newNotFound("Kategori tidak ditemukan")
	case errors.Is(err, model.ErrDuplicateName):
		return newConflict("Nama kategori sudah digunakan")
	default:
		return err
	}
}
