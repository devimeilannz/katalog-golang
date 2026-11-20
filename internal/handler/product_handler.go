
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"errors"
    "io"

	"katalog-api/internal/model"
	"katalog-api/internal/service"
)

var (
	jsonNumberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
	priceFilterPattern = regexp.MustCompile(`^\d{1,15}(\.\d{1,6})?$`)
)

type ProductService interface {
	Create(ctx context.Context, in model.ProductInput) (*model.ProductDetail, error)
	List(ctx context.Context, f model.ProductFilter) ([]model.ProductDetail, model.Meta, error)
	Get(ctx context.Context, id string) (*model.ProductDetail, error)
	Update(ctx context.Context, id string, in model.ProductInput) (*model.ProductDetail, error)
	Patch(ctx context.Context, id string, in model.ProductPatchInput) (*model.ProductDetail, error)
	Delete(ctx context.Context, id string) error
	BulkCreate(ctx context.Context, items []model.ProductInput) ([]model.ProductDetail, error)
}

type ProductHandler struct {
	svc ProductService
}

func NewProductHandler(svc ProductService) *ProductHandler {
	return &ProductHandler{svc: svc}
}

func (h *ProductHandler) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.create(w, r)
	case http.MethodGet:
		h.list(w, r)
	default:
		notFound(w, r)
	}
}

func (h *ProductHandler) Item(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.get(w, r)
	case http.MethodPut:
		h.update(w, r)
		case http.MethodPatch:
	     h.patch(w, r)
	case http.MethodDelete:
		h.delete(w, r)
	default:
		notFound(w, r)
	}
}

func (h *ProductHandler) create(w http.ResponseWriter, r *http.Request) {
	in, err := decodeProduct(w, r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := h.svc.Create(r.Context(), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusCreated, "Produk berhasil dibuat", d.ToResponse())
}


func (h *ProductHandler) list(w http.ResponseWriter, r *http.Request) {
    f, err := parseProductFilter(r)
    if err != nil {
        writeError(w, r, err)
        return
    }

    items, meta, err := h.svc.List(r.Context(), f)
    if err != nil {
        writeError(w, r, err)
        return
    }

    message := "Daftar produk berhasil diambil"
    if len(items) == 0 {
        message = "Produk yang dicari tidak ditemukan"
    }

    writeSuccessMeta(
        w,
        http.StatusOK,
        message,
        toProductResponses(items),
        meta,
    )
}


func (h *ProductHandler) get(w http.ResponseWriter, r *http.Request) {
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
	writeSuccess(w, http.StatusOK, "Detail produk berhasil diambil", d.ToResponse())
}

func (h *ProductHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	in, err := decodeProduct(w, r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := h.svc.Update(r.Context(), id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusOK, "Produk berhasil diperbarui", d.ToResponse())
}


func (h *ProductHandler) patch(w http.ResponseWriter, r *http.Request) {
    id, err := parseID(r)
    if err != nil {
        writeError(w, r, err)
        return
    }

    var in model.ProductPatchInput
    if err := decodeJSON(w, r, &in); err != nil {
        writeError(w, r, err)
        return
    }

    d, err := h.svc.Patch(r.Context(), id, in)
    if err != nil {
        writeError(w, r, err)
        return
    }

    writeSuccess(w, http.StatusOK, "Produk berhasil diperbarui", d.ToResponse())
}


func (h *ProductHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusOK, "Produk berhasil dihapus", model.DeletedResponse{ID: id})
}

// Bulk melayani POST /products/bulk.
func (h *ProductHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		notFound(w, r)
		return
	}
	var body struct {
		Products []json.RawMessage `json:"products"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}

	inputs := make([]model.ProductInput, len(body.Products))
	var details []service.FieldError
	for i, raw := range body.Products {
		idx := i
		var pb productBody
		if err := json.Unmarshal(raw, &pb); err != nil {
			details = append(details, service.FieldError{
				Index: &idx, Field: "products",
				Message: "harus berupa objek JSON",
			})
			continue
		}
		in, errs := pb.toInput()
		for _, e := range errs {
			n := i
			e.Index = &n
			details = append(details, e)
		}
		inputs[i] = in
	}
	if len(details) > 0 {
		writeError(w, r, service.NewBulkValidationError(details))
		return
	}

	created, err := h.svc.BulkCreate(r.Context(), inputs)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeSuccess(w, http.StatusCreated,
		fmt.Sprintf("%d produk berhasil dibuat", len(created)),
		toProductResponses(created))
}

func toProductResponses(items []model.ProductDetail) []model.ProductResponse {
	out := make([]model.ProductResponse, 0, len(items))
	for _, p := range items {
		out = append(out, p.ToResponse())
	}
	return out
}

// productBody menerima setiap field dalam bentuk json.RawMessage.
type productBody struct {
	SKU         json.RawMessage `json:"sku"`
	Name        json.RawMessage `json:"name"`
	Description json.RawMessage `json:"description"`
	Price       json.RawMessage `json:"price"`
	Stock       json.RawMessage `json:"stock"`
	CategoryID  json.RawMessage `json:"category_id"`
	IsActive    json.RawMessage `json:"is_active"`
}

// toInput memeriksa tipe setiap field.
func (b productBody) toInput() (model.ProductInput, []service.FieldError) {
	var errs []service.FieldError
	in := model.ProductInput{
		SKU:         rawString(b.SKU, "sku", &errs),
		Name:        rawString(b.Name, "name", &errs),
		Description: rawString(b.Description, "description", &errs),
		Price:       rawNumber(b.Price, "price", &errs),
		Stock:       rawNumber(b.Stock, "stock", &errs),
		CategoryID:  rawString(b.CategoryID, "category_id", &errs),
		IsActive:    rawBool(b.IsActive, "is_active", &errs),
	}
	return in, errs
}
//memeriksa apakah suatu field kosong atau tidak
func isAbsent(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

func rawString(raw json.RawMessage, field string, errs *[]service.FieldError) *string {
	if isAbsent(raw) {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		*errs = append(*errs, service.FieldError{
			Field: field, Message: "harus berupa string",
		})
		return nil
	}
	return &s
}

func rawNumber(raw json.RawMessage, field string, errs *[]service.FieldError) *string {
	if isAbsent(raw) {
		return nil
	}
	s := string(bytes.TrimSpace(raw))
	if !jsonNumberPattern.MatchString(s) {
		*errs = append(*errs, service.FieldError{
			Field: field, Message: "harus berupa angka",
		})
		return nil
	}
	return &s
}

func rawBool(raw json.RawMessage, field string, errs *[]service.FieldError) *bool {
	if isAbsent(raw) {
		return nil
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		*errs = append(*errs, service.FieldError{
			Field: field, Message: "harus berupa boolean",
		})
		return nil
	}
	return &v
}

// decodeProduct membaca body POST/PUT produk.
func decodeProduct(w http.ResponseWriter, r *http.Request) (model.ProductInput, error) {
	var body productBody
	if err := decodeJSON(w, r, &body); err != nil {
		return model.ProductInput{}, err
	}
	in, errs := body.toInput()
	if len(errs) > 0 {
		return model.ProductInput{}, service.NewValidationError(errs)
	}
	return in, nil
}

// parseProductFilter membaca query parameter GET /products.
func parseProductFilter(r *http.Request) (model.ProductFilter, error) {
	q := r.URL.Query()
	f := model.ProductFilter{
		Page: defaultPage, Limit: defaultLimit,
		Search: strings.TrimSpace(q.Get("search")),
		Sort: "created_at", Order: "desc",
	}
	var details []service.FieldError
	bad := func(field, msg string) {
		details = append(details, service.FieldError{Field: field, Message: msg})
	}

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPage {
			bad("page", "harus berupa bilangan bulat 1-1000000")
		} else {
			f.Page = n
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			bad("limit", "harus berupa bilangan bulat 1-100")
		} else {
			f.Limit = n
		}
	}
	if v := q.Get("category_id"); v != "" {
		if !model.IsUUID(v) {
			bad("category_id", "harus berupa UUID")
		} else {
			f.CategoryID = &v
		}
	}

	var minOK, maxOK bool
	if v := q.Get("min_price"); v != "" {
		if !priceFilterPattern.MatchString(v) {
			bad("min_price", "harus berupa angka tidak negatif")
		} else {
			f.MinPrice, minOK = &v, true
		}
	}
	if v := q.Get("max_price"); v != "" {
		if !priceFilterPattern.MatchString(v) {
			bad("max_price", "harus berupa angka tidak negatif")
		} else {
			f.MaxPrice, maxOK = &v, true
		}
	}
	if minOK && maxOK {
		lo, _ := new(big.Rat).SetString(*f.MinPrice)
		hi, _ := new(big.Rat).SetString(*f.MaxPrice)
		if lo.Cmp(hi) > 0 {
			bad("min_price", "tidak boleh lebih besar dari max_price")
		}
	}

	switch v := q.Get("is_active"); v {
	case "":
	case "true", "false":
		b := v == "true"
		f.IsActive = &b
	default:
		bad("is_active", "harus true atau false")
	}

	switch v := q.Get("sort"); v {
	case "":
	case "name", "price", "created_at":
		f.Sort = v
	default:
		bad("sort", "harus salah satu dari: name, price, created_at")
	}
	switch v := q.Get("order"); v {
	case "":
	case "asc", "desc":
		f.Order = v
	default:
		bad("order", "harus asc atau desc")
	}

	if len(details) > 0 {
		return model.ProductFilter{}, service.NewValidationError(details)
	}
	return f, nil
}
// decodeJSON membaca satu objek JSON dari request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
    r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
    dec := json.NewDecoder(r.Body)

    if err := dec.Decode(dst); err != nil {
        return bodyError(err)
    }

    // Tolak data tambahan setelah objek JSON.
    if _, err := dec.Token(); !errors.Is(err, io.EOF) {
        return service.NewValidationError([]service.FieldError{
            {Field: "body", Message: "body harus berisi tepat satu objek JSON"},
        })
    }

    return nil
}
