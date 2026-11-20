package service

import "errors"

var (
	ErrValidation = errors.New("validation error")
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
)

// FieldError menjelaskan satu pelanggaran validasi.
type FieldError struct {
	 Index   *int   `json:"index,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error adalah error service yang membawa pesan untuk client.
// Kind berisi salah satu sentinel di atas, sehingga errors.Is(err, ErrNotFound) bekerja.
type Error struct {
	Kind    error
	Message string
	Details []FieldError
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Kind }

// NewValidationError membuat error validasi yang berisi seluruh pelanggaran sekaligus.
func NewValidationError(details []FieldError) *Error {
	return &Error{Kind: ErrValidation, Message: "Validasi gagal", Details: details}
}

func newNotFound(msg string) *Error { return &Error{Kind: ErrNotFound, Message: msg} }
func newConflict(msg string) *Error { return &Error{Kind: ErrConflict, Message: msg} }

// NewNotFoundError dipakai handler untuk route yang tidak dikenal.
func NewNotFoundError(msg string) *Error { return newNotFound(msg) }

// NewBulkValidationError membuat error validasi untuk proses bulk.
func NewBulkValidationError(details []FieldError) *Error {
    return &Error{
        Kind:    ErrValidation,
        Message: "Validasi bulk gagal",
        Details: details,
    }
}

// newBulkConflict membuat error konflik untuk proses bulk.
func newBulkConflict(details []FieldError) *Error {
    return &Error{
        Kind:    ErrConflict,
        Message: "Terdapat konflik pada data bulk",
        Details: details,
    }
}
