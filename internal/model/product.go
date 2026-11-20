package model

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var (
	ErrDuplicateSKU     = errors.New("sku duplikat")
	ErrCategoryNotFound = errors.New("kategori tidak ditemukan")
)

type ProductInput struct {
	SKU         *string
	Name        *string
	Description *string
	Price       *string
	Stock       *string
	CategoryID  *string
	IsActive    *bool
}


type ProductPatchInput struct {
    SKU         json.RawMessage `json:"sku"`
    Name        json.RawMessage `json:"name"`
    Description json.RawMessage `json:"description"`
    Price       json.RawMessage `json:"price"`
    Stock       json.RawMessage `json:"stock"`
    CategoryID  json.RawMessage `json:"category_id"`
    IsActive    json.RawMessage `json:"is_active"`
}


// Product adalah data produk yang sudah tervalidasi dan siap disimpan.
type Product struct {
	SKU         string
	Name        string
	Description *string
	Price       string
	Stock       int
	CategoryID  string
	IsActive    bool
}

type ProductDetail struct {
	ID           string
	SKU          string
	Name         string
	Description  *string
	Price        string
	Stock        int
	IsActive     bool
	CategoryID   string
	CategoryName string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ProductFilter struct {
	Page       int
	Limit      int
	Search     string
	CategoryID *string
	MinPrice   *string
	MaxPrice   *string
	IsActive   *bool
	Sort       string
	Order      string
	
}

type CategoryRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProductResponse struct {
	ID          string      `json:"id"`
	SKU         string      `json:"sku"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Price       json.Number `json:"price"`
	Stock       int         `json:"stock"`
	IsActive    bool        `json:"is_active"`
	Category    CategoryRef `json:"category"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

func (p ProductDetail) ToResponse() ProductResponse {
	return ProductResponse{
		ID:          p.ID,
		SKU:         p.SKU,
		Name:        p.Name,
		Description: p.Description,
		Price:       json.Number(p.Price),
		Stock:       p.Stock,
		IsActive:    p.IsActive,
		Category: CategoryRef{
			ID:   p.CategoryID,
			Name: p.CategoryName,
		},
		CreatedAt: formatTime(p.CreatedAt),
		UpdatedAt: formatTime(p.UpdatedAt),
	}
}
func IsUUID(value string) bool {
    matched, err := regexp.MatchString(
        `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
        value,
    )
    return err == nil && matched
}
