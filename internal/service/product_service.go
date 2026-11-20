package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"katalog-api/internal/model"
)

const (
	skuMaxLen         = 50
	productNameMin    = 3
	productNameMax    = 150
	productDescMax    = 1000
	priceMaxIntDigits = 13
	bulkMin           = 1
	bulkMax           = 50
)

var (
	skuPattern     = regexp.MustCompile(`^[A-Z0-9-]+$`)
	decimalPattern = regexp.MustCompile(`^(-?)(\d+)(?:\.(\d+))?$`)
	integerPattern = regexp.MustCompile(`^-?\d+$`)
)

type ProductRepository interface {
	CategoryExists(ctx context.Context, categoryID string) (bool, error)
	ExistingSKUs(ctx context.Context, skus []string) (map[string]bool, error)
	Create(ctx context.Context, p model.Product) (*model.ProductDetail, error)
	List(ctx context.Context, f model.ProductFilter) ([]model.ProductDetail, int, error)
	GetByID(ctx context.Context, id string) (*model.ProductDetail, error)
	LockProduct(ctx context.Context, id string) error
	Update(ctx context.Context, id string, p model.Product) (*model.ProductDetail, error)
	SoftDelete(ctx context.Context, id string) error

	WithTx(ctx context.Context, fn func(tx ProductRepository) error) error
	LockCategory(ctx context.Context, categoryID string) error
}

type ProductService struct {
	repo ProductRepository
}

func NewProductService(repo ProductRepository) *ProductService {
	return &ProductService{repo: repo}
}

func (s *ProductService) Create(ctx context.Context, in model.ProductInput) (*model.ProductDetail, error) {
	p, details := validateProduct(in, false)
	details, err := s.checkCategory(ctx, p.CategoryID, details, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(details) > 0 {
		return nil, NewValidationError(details)
	}

	var out *model.ProductDetail
	err = s.repo.WithTx(ctx, func(tx ProductRepository) error {
		if err := tx.LockCategory(ctx, p.CategoryID); err != nil {
			return err
		}
		d, err := tx.Create(ctx, p)
		out = d
		return err
	})
	if err != nil {
		return nil, mapProductError(err)
	}
	return out, nil
}

// List mengembalikan daftar produk aktif beserta meta pagination.
func (s *ProductService) List(ctx context.Context, f model.ProductFilter) ([]model.ProductDetail, model.Meta, error) {
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, model.Meta{}, mapProductError(err)
	}
	if items == nil {
		items = []model.ProductDetail{}
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + f.Limit - 1) / f.Limit
	}
	return items, model.Meta{
		Page:       f.Page,
		Limit:      f.Limit,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

// Get mengembalikan detail produk beserta nama kategorinya.
func (s *ProductService) Get(ctx context.Context, id string) (*model.ProductDetail, error) {
	d, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapProductError(err)
	}
	return d, nil
}

func (s *ProductService) Update(ctx context.Context, id string, in model.ProductInput) (*model.ProductDetail, error) {
	p, details := validateProduct(in, true)
	details, err := s.checkCategory(ctx, p.CategoryID, details, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(details) > 0 {
		return nil, NewValidationError(details)
	}

	var out *model.ProductDetail
	err = s.repo.WithTx(ctx, func(tx ProductRepository) error {
    if err := tx.LockProduct(ctx, id); err != nil {
        return err
    }

    if err := tx.LockCategory(ctx, p.CategoryID); err != nil {
        return err
    }

    d, err := tx.Update(ctx, id, p)
    out = d
    return err
})
	if err != nil {
		return nil, mapProductError(err)
	}
	return out, nil
}

// Patch memperbarui sebagian field produk (PATCH).
func (s *ProductService) Patch(ctx context.Context, id string, in model.ProductPatchInput) (*model.ProductDetail, error) {
	var out *model.ProductDetail

	err := s.repo.WithTx(ctx, func(tx ProductRepository) error {
		// 1. Kunci produk agar tidak diubah transaksi lain.
		if err := tx.LockProduct(ctx, id); err != nil {
			return err
		}

		// 2. Ambil data produk terbaru.
		d, err := tx.GetByID(ctx, id)
		if err != nil {
			return err
		}

		// 3. Ubah ProductDetail menjadi Product.
		current := productFromDetail(d)

		// 4. Gabungkan data lama dengan field PATCH.
		updated, err := applyProductPatch(current, in)
		if err != nil {
			return err
		}

		// 5. Validasi seluruh hasil gabungan.
		input := model.ProductInput{
			SKU:         &updated.SKU,
			Name:        &updated.Name,
			Description: updated.Description,
			Price:       &updated.Price,
			Stock:       stringPointer(strconv.Itoa(updated.Stock)),
			CategoryID:  &updated.CategoryID,
			IsActive:    &updated.IsActive,
		}

		validated, details := validateProduct(input, true)
		if len(details) > 0 {
			return NewValidationError(details)
		}

		// 6. Kunci kategori tujuan.
		if err := tx.LockCategory(ctx, validated.CategoryID); err != nil {
			return err
		}

		// 7. Simpan perubahan.
		out, err = tx.Update(ctx, id, validated)
		return err
	})

	if err != nil {
		return nil, mapProductError(err)
	}

	return out, nil
}

func (s *ProductService) Delete(ctx context.Context, id string) error {
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		return mapProductError(err)
	}
	return nil
}

func (s *ProductService) BulkCreate(ctx context.Context, items []model.ProductInput) ([]model.ProductDetail, error) {
	if len(items) < bulkMin || len(items) > bulkMax {
		return nil, NewValidationError([]FieldError{
			{Field: "products", Message: "harus berisi 1-50 produk"},
		})
	}

	// Tahap 1: validasi semua item.
	products := make([]model.Product, len(items))
	var details []FieldError
	exists := map[string]bool{}

	for i, in := range items {
		p, errs := validateProduct(in, false)
		products[i] = p
		idx := i
		var err error
		errs, err = s.checkCategory(ctx, p.CategoryID, errs, exists, &idx)
		if err != nil {
			return nil, err
		}
		for _, e := range errs {
			if e.Index == nil {
				n := i
				e.Index = &n
			}
			details = append(details, e)
		}
	}
	if len(details) > 0 {
		return nil, NewBulkValidationError(details)
	}

	// Tahap 2: periksa SKU duplikat.
	if conflicts, err := s.bulkConflicts(ctx, products); err != nil {
		return nil, err
	} else if len(conflicts) > 0 {
		return nil, newBulkConflict(conflicts)
	}

	// Tahap 3: simpan semua produk dalam satu transaksi.
	var created []model.ProductDetail
	err := s.repo.WithTx(ctx, func(tx ProductRepository) error {
		locked := map[string]bool{}
		for i, p := range products {
			if !locked[p.CategoryID] {
				if err := tx.LockCategory(ctx, p.CategoryID); err != nil {
					return &bulkItemError{Index: i, Err: err}
				}
				locked[p.CategoryID] = true
			}
			d, err := tx.Create(ctx, p)
			if err != nil {
				return &bulkItemError{Index: i, Err: err}
			}
			created = append(created, *d)
		}
		return nil
	})
	if err != nil {
		return nil, mapBulkError(err)
	}
	return created, nil
}

// bulkConflicts mencari SKU kembar dalam request atau database.
func (s *ProductService) bulkConflicts(ctx context.Context, products []model.Product) ([]FieldError, error) {
	var conflicts []FieldError
	seen := make(map[string]int, len(products))
	skus := make([]string, 0, len(products))

	for i, p := range products {
		if first, dup := seen[p.SKU]; dup {
			idx := i
			conflicts = append(conflicts, FieldError{
				Index:   &idx,
				Field:   "sku",
				Message: fmt.Sprintf("duplikat dengan item index %d", first),
			})
			continue
		}
		seen[p.SKU] = i
		skus = append(skus, p.SKU)
	}

	taken, err := s.repo.ExistingSKUs(ctx, skus)
	if err != nil {
		return nil, err
	}
	for i, p := range products {
		if taken[p.SKU] {
			idx := i
			conflicts = append(conflicts, FieldError{
				Index:   &idx,
				Field:   "sku",
				Message: "sudah dipakai",
			})
		}
	}
	sort.SliceStable(conflicts, func(a, b int) bool {
		return *conflicts[a].Index < *conflicts[b].Index
	})
	return conflicts, nil
}

// checkCategory memeriksa apakah kategori tersedia dan belum dihapus.
func (s *ProductService) checkCategory(ctx context.Context, categoryID string, details []FieldError, cache map[string]bool, index *int) ([]FieldError, error) {
	if categoryID == "" {
		return details, nil
	}
	ok, cached := cache[categoryID]
	if !cached {
		var err error
		if ok, err = s.repo.CategoryExists(ctx, categoryID); err != nil {
			return nil, err
		}
		if cache != nil {
			cache[categoryID] = ok
		}
	}
	if !ok {
		details = append(details, FieldError{
			Index:   index,
			Field:   "category_id",
			Message: "kategori tidak ditemukan",
		})
	}
	return details, nil
}

// validateProduct menerapkan aturan validasi produk.
func validateProduct(in model.ProductInput, requireAll bool) (model.Product, []FieldError) {
	var (
		p    model.Product
		errs []FieldError
	)
	add := func(field, msg string) {
		errs = append(errs, FieldError{Field: field, Message: msg})
	}

	// sku
	switch {
	case in.SKU == nil || *in.SKU == "":
		add("sku", "wajib diisi")
	case !skuPattern.MatchString(*in.SKU):
		add("sku", "hanya boleh huruf besar, angka, dan tanda hubung")
	case len(*in.SKU) > skuMaxLen:
		add("sku", "maksimal 50 karakter")
	default:
		p.SKU = *in.SKU
	}

	// name
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		add("name", "wajib diisi")
	} else if n := utf8.RuneCountInString(strings.TrimSpace(*in.Name)); n < productNameMin || n > productNameMax {
		add("name", "harus 3-150 karakter")
	} else {
		p.Name = strings.TrimSpace(*in.Name)
	}

	// description
	switch {
	case in.Description == nil:
		if requireAll {
			add("description", "wajib dikirim")
		}
	case utf8.RuneCountInString(*in.Description) > productDescMax:
		add("description", "maksimal 1000 karakter")
	default:
		p.Description = in.Description
	}

	// price
	if in.Price == nil {
		add("price", "wajib diisi")
	} else if price, msg := parsePrice(*in.Price); msg != "" {
		add("price", msg)
	} else {
		p.Price = price
	}

	// stock
	if in.Stock == nil {
		add("stock", "wajib diisi")
	} else if stock, msg := parseStock(*in.Stock); msg != "" {
		add("stock", msg)
	} else {
		p.Stock = stock
	}

	// category_id
	switch {
	case in.CategoryID == nil || *in.CategoryID == "":
		add("category_id", "wajib diisi")
	case !model.IsUUID(*in.CategoryID):
		add("category_id", "harus berupa UUID")
	default:
		p.CategoryID = *in.CategoryID
	}

	// is_active
	switch {
	case in.IsActive != nil:
		p.IsActive = *in.IsActive
	case requireAll:
		add("is_active", "wajib dikirim")
	default:
		p.IsActive = true
	}

	return p, errs
}

// parsePrice memeriksa angka harga dan mengembalikan format desimal baku.
func parsePrice(s string) (string, string) {
	m := decimalPattern.FindStringSubmatch(s)
	if m == nil {
		return "", "harus berupa angka"
	}
	negative := m[1] == "-"
	intPart := strings.TrimLeft(m[2], "0")
	frac := strings.TrimRight(m[3], "0")
	if intPart == "" {
		intPart = "0"
	}
	if negative || (intPart == "0" && frac == "") {
		return "", "harus lebih dari 0"
	}
	if len(frac) > 2 {
		return "", "maksimal 2 desimal"
	}
	if len(intPart) > priceMaxIntDigits {
		return "", "terlalu besar (maksimal 13 digit sebelum desimal)"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	return intPart + "." + frac, ""
}

// parseStock memeriksa stok sebagai bilangan bulat.
func parseStock(s string) (int, string) {
	if !integerPattern.MatchString(s) {
		if decimalPattern.MatchString(s) || strings.ContainsAny(s, "eE") {
			return 0, "harus bilangan bulat"
		}
		return 0, "harus berupa angka"
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		if strings.HasPrefix(s, "-") {
			return 0, "minimal 0"
		}
		return 0, fmt.Sprintf("maksimal %d", math.MaxInt32)
	}
	if n < 0 {
		return 0, "minimal 0"
	}
	if n > math.MaxInt32 {
		return 0, fmt.Sprintf("maksimal %d", math.MaxInt32)
	}
	return int(n), ""
}
// decodePatchField membaca field dari request PATCH.
func decodePatchField[T any](raw json.RawMessage, field string) (T, bool, error) {
    var value T

    // Field tidak dikirim.
    if len(raw) == 0 {
        return value, false, nil
    }

    // Field dikirim dengan nilai null.
    if string(raw) == "null" {
        return value, true, fmt.Errorf("%s tidak boleh null", field)
    }

    // Mengubah JSON menjadi tipe data tujuan.
    if err := json.Unmarshal(raw, &value); err != nil {
        return value, true, fmt.Errorf("%s memiliki format yang tidak valid", field)
    }

    return value, true, nil
}

// productFromDetail mengubah ProductDetail menjadi Product.
func productFromDetail(d *model.ProductDetail) model.Product {
	return model.Product{
		SKU:         d.SKU,
		Name:        d.Name,
		Description: d.Description,
		Price:       d.Price,
		Stock:       d.Stock,
		CategoryID:  d.CategoryID,
		IsActive:    d.IsActive,
	}
}

func applyProductPatch(current model.Product, in model.ProductPatchInput) (model.Product, error) {
	var errs []FieldError

	add := func(field, msg string) {
		errs = append(errs, FieldError{
			Field:   field,
			Message: msg,
		})
	}

	// SKU
	if value, sent, err := decodePatchField[string](in.SKU, "sku"); err != nil {
		add("sku", "tidak boleh null atau format tidak valid")
	} else if sent {
		current.SKU = value
	}

	// Name
	if value, sent, err := decodePatchField[string](in.Name, "name"); err != nil {
		add("name", "tidak boleh null atau format tidak valid")
	} else if sent {
		current.Name = value
	}

	// Description: null diperbolehkan untuk menghapus deskripsi.
	if len(in.Description) > 0 {
		if string(in.Description) == "null" {
			current.Description = nil
		} else {
			value, _, err := decodePatchField[string](in.Description, "description")
			if err != nil {
				add("description", "format tidak valid")
			} else {
				current.Description = &value
			}
		}
	}

	// Price
	if value, sent, err := decodePatchField[string](in.Price, "price"); err != nil {
		add("price", "tidak boleh null atau format tidak valid")
	} else if sent {
		current.Price = value
	}

	// Stock
	if value, sent, err := decodePatchField[string](in.Stock, "stock"); err != nil {
		add("stock", "tidak boleh null atau format tidak valid")
	} else if sent {
		current.Stock = 0
		if stock, msg := parseStock(value); msg != "" {
			add("stock", msg)
		} else {
			current.Stock = stock
		}
	}

	// Category ID
	if value, sent, err := decodePatchField[string](in.CategoryID, "category_id"); err != nil {
		add("category_id", "tidak boleh null atau format tidak valid")
	} else if sent {
		current.CategoryID = value
	}

	// Is Active
	if value, sent, err := decodePatchField[bool](in.IsActive, "is_active"); err != nil {
		add("is_active", "tidak boleh null atau format tidak valid")
	} else if sent {
		current.IsActive = value
	}

	if len(errs) > 0 {
		return current, NewValidationError(errs)
	}

	return current, nil
}
// stringPointer mengubah string menjadi pointer string.
func stringPointer(value string) *string {
	return &value
}


// bulkItemError membungkus error repository dengan index item.
type bulkItemError struct {
	Index int
	Err   error
}

func (e *bulkItemError) Error() string {
	return fmt.Sprintf("item %d: %v", e.Index, e.Err)
}

func (e *bulkItemError) Unwrap() error {
	return e.Err
}

// mapBulkError menerjemahkan kegagalan dalam transaksi bulk.
func mapBulkError(err error) error {
	var item *bulkItemError
	if !errors.As(err, &item) {
		return err
	}
	idx := item.Index
	switch {
	case errors.Is(item.Err, model.ErrDuplicateSKU):
		return newBulkConflict([]FieldError{{
			Index: &idx, Field: "sku", Message: "sudah dipakai",
		}})
	case errors.Is(item.Err, model.ErrCategoryNotFound):
		return NewBulkValidationError([]FieldError{{
			Index: &idx, Field: "category_id", Message: "kategori tidak ditemukan",
		}})
	default:
		return item.Err
	}
}

// mapProductError menerjemahkan error repository ke error service.
func mapProductError(err error) error {
	var svcErr *Error
	switch {
	case errors.As(err, &svcErr):
		return err
	case errors.Is(err, model.ErrRecordNotFound):
		return newNotFound("Produk tidak ditemukan")
	case errors.Is(err, model.ErrDuplicateSKU):
		return newConflict("SKU sudah dipakai")
	case errors.Is(err, model.ErrCategoryNotFound):
		return NewValidationError([]FieldError{{
			Field: "category_id", Message: "kategori tidak ditemukan",
		}})
	default:
		return err
	}
}
