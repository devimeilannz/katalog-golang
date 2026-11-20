package model

import (
	"errors"
	"time"
)

var (
	ErrRecordNotFound = errors.New("record tidak ditemukan")
	ErrDuplicateName  = errors.New("nama kategori duplikat")
)

type Category struct {
	ID          string
	Name        string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CategoryDetail struct {
	Category
	ProductCount int
}

// Memakai pointer agar bisa membedakan field yang tidak dikirim dengan string kosong.
type CategoryRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type ListParams struct {
	Page   int
	Limit  int
	Search string
}

type Meta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	TotalItems int `json:"total_items"`
	TotalPages int `json:"total_pages"`
}

type CategoryResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type CategoryDetailResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	ProductCount int     `json:"product_count"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type DeletedResponse struct {
	ID string `json:"id"`
}

const timeLayout = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func (c Category) ToResponse() CategoryResponse {
	return CategoryResponse{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		CreatedAt:   formatTime(c.CreatedAt),
		UpdatedAt:   formatTime(c.UpdatedAt),
	}
}

func (c CategoryDetail) ToResponse() CategoryDetailResponse {
	return CategoryDetailResponse{
		ID:           c.ID,
		Name:         c.Name,
		Description:  c.Description,
		ProductCount: c.ProductCount,
		CreatedAt:    formatTime(c.CreatedAt),
		UpdatedAt:    formatTime(c.UpdatedAt),
	}
}
