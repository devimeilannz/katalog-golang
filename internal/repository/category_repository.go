// Package repository berisi query SQL ke PostgreSQL.
// Package ini tidak boleh tahu soal HTTP.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"

	"katalog-api/internal/model"
	"katalog-api/internal/service"
)

// pgUniqueViolation adalah SQLSTATE untuk pelanggaran unique constraint.
const pgUniqueViolation = "23505"

// dbtx adalah method yang dipakai bersama oleh *sql.DB dan *sql.Tx.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CategoryRepository mengimplementasikan service.CategoryRepository.
type CategoryRepository struct {
	db      *sql.DB
	q       dbtx
	timeout time.Duration
}

var _ service.CategoryRepository = (*CategoryRepository)(nil)

// NewCategoryRepository membuat repository. timeout berlaku untuk setiap query.
func NewCategoryRepository(db *sql.DB, timeout time.Duration) *CategoryRepository {
	return &CategoryRepository{db: db, q: db, timeout: timeout}
}

// withTimeout memasang timeout (misalnya 5 detik) di atas context request.
func (r *CategoryRepository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

func (r *CategoryRepository) Create(ctx context.Context, name string, description *string) (*model.Category, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	const query = `
		INSERT INTO categories (name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at, updated_at`
	c, err := scanCategory(r.q.QueryRowContext(ctx, query, name, description))
	if err != nil {
		return nil, translateErr(err)
	}
	return c, nil
}

func (r *CategoryRepository) List(ctx context.Context, search string, limit, offset int) ([]model.Category, int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	pattern := escapeLike(search)

	var total int
	const countQuery = `
		SELECT COUNT(*) FROM categories
		WHERE deleted_at IS NULL
		  AND ($1::text = '' OR name ILIKE '%' || $1::text || '%' ESCAPE '\')`
	if err := r.q.QueryRowContext(ctx, countQuery, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}

	const listQuery = `
		SELECT id, name, description, created_at, updated_at
		FROM categories
		WHERE deleted_at IS NULL
		  AND ($1::text = '' OR name ILIKE '%' || $1::text || '%' ESCAPE '\')
		ORDER BY created_at DESC, id
		LIMIT $2 OFFSET $3`
	rows, err := r.q.QueryContext(ctx, listQuery, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.Category, 0, limit)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *CategoryRepository) GetByID(ctx context.Context, id string) (*model.CategoryDetail, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	const query = `
    SELECT c.id, c.name, c.description, c.created_at, c.updated_at,
           (
               SELECT COUNT(*)
               FROM products p
               WHERE p.category_id = c.id
                 AND p.deleted_at IS NULL
           ) AS product_count
    FROM categories c
    WHERE c.id = $1 AND c.deleted_at IS NULL`
	var (
		d    model.CategoryDetail
		desc sql.NullString
	)
	err := r.q.QueryRowContext(ctx, query, id).Scan(
		&d.ID, &d.Name, &desc, &d.CreatedAt, &d.UpdatedAt, &d.ProductCount)
	if err != nil {
		return nil, translateErr(err)
	}
	d.Description = nullToPtr(desc)
	return &d, nil
}

func (r *CategoryRepository) Update(ctx context.Context, id, name string, description *string) (*model.Category, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	const query = `
		UPDATE categories
		SET name = $2, description = $3, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, name, description, created_at, updated_at`
	c, err := scanCategory(r.q.QueryRowContext(ctx, query, id, name, description))
	if err != nil {
		return nil, translateErr(err)
	}
	return c, nil
}

// WithTx menjalankan fn dalam satu transaksi.
func (r *CategoryRepository) WithTx(ctx context.Context, fn func(tx service.CategoryRepository) error) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txRepo := &CategoryRepository{db: r.db, q: tx, timeout: r.timeout}
	if err := fn(txRepo); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *CategoryRepository) LockActive(ctx context.Context, id string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var got string
	err := r.q.QueryRowContext(ctx,
		`SELECT id FROM categories WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&got)
	return translateErr(err)
}

// CountActiveProducts menghitung produk aktif dalam suatu kategori.
func (r *CategoryRepository) CountActiveProducts(ctx context.Context, categoryID string) (int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	const query = `
        SELECT COUNT(*)
        FROM products
        WHERE category_id = $1
          AND deleted_at IS NULL`

	var total int
	err := r.q.QueryRowContext(ctx, query, categoryID).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}

func (r *CategoryRepository) SoftDelete(ctx context.Context, id string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	res, err := r.q.ExecContext(ctx,
		`UPDATE categories SET deleted_at = now(), updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrRecordNotFound
	}
	return nil
}

// Ping dipakai health check untuk memastikan database terjangkau.
func (r *CategoryRepository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.db.PingContext(ctx)
}

// scanner dipenuhi oleh *sql.Row dan *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanCategory(s scanner) (*model.Category, error) {
	var (
		c    model.Category
		desc sql.NullString
	)
	if err := s.Scan(&c.ID, &c.Name, &desc, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.Description = nullToPtr(desc)
	return &c, nil
}

func nullToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

// translateErr mengubah error driver menjadi error domain.
func translateErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrRecordNotFound
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == pgUniqueViolation {
		return model.ErrDuplicateName
	}
	return err
}

// escapeLike meng-escape karakter spesial LIKE agar pencarian "50%" dicari apa adanya.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
