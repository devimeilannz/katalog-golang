
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"katalog-api/internal/model"
	"katalog-api/internal/service"
)

const (
	pgForeignKeyViolation = "23503"
	skuIndexName = "uq_products_sku_active"
)

const productColumns = `p.id, p.sku, p.name, p.description, p.price::text, p.stock, p.is_active,
		       c.id, c.name, p.created_at, p.updated_at`

var sortColumns = map[string]string{
	"name":       "p.name",
	"price":      "p.price",
	"created_at": "p.created_at",
}

type ProductRepository struct {
	db      *sql.DB
	q       dbtx
	timeout time.Duration
}

var _ service.ProductRepository = (*ProductRepository)(nil)

func NewProductRepository(db *sql.DB, timeout time.Duration) *ProductRepository {
	return &ProductRepository{db: db, q: db, timeout: timeout}
}

func (r *ProductRepository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

func (r *ProductRepository) CategoryExists(ctx context.Context, categoryID string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var ok bool
	err := r.q.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM categories WHERE id = $1 AND deleted_at IS NULL)`, categoryID).Scan(&ok)
	return ok, err
}

func (r *ProductRepository) ExistingSKUs(ctx context.Context, skus []string) (map[string]bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	rows, err := r.q.QueryContext(ctx,
		`SELECT sku FROM products WHERE sku = ANY($1) AND deleted_at IS NULL`, pq.Array(skus))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	taken := make(map[string]bool)
	for rows.Next() {
		var sku string
		if err := rows.Scan(&sku); err != nil {
			return nil, err
		}
		taken[sku] = true
	}
	return taken, rows.Err()
}

func (r *ProductRepository) Create(ctx context.Context, p model.Product) (*model.ProductDetail, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	const query = `
		WITH p AS (
			INSERT INTO products (sku, name, description, price, stock, category_id, is_active)
			VALUES ($1, $2, $3, $4::numeric, $5, $6, $7)
			RETURNING *
		)
		SELECT ` + productColumns + `
		FROM p JOIN categories c ON c.id = p.category_id`

	d, err := scanProduct(r.q.QueryRowContext(ctx, query,
		p.SKU, p.Name, p.Description, p.Price, p.Stock, p.CategoryID, p.IsActive))
	if err != nil {
		return nil, translateProductErr(err)
	}
	return d, nil
}

func (r *ProductRepository) Update(ctx context.Context, id string, p model.Product) (*model.ProductDetail, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	const query = `
		WITH p AS (
			UPDATE products
			SET sku = $2, name = $3, description = $4, price = $5::numeric, stock = $6,
			    category_id = $7, is_active = $8, updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL
			RETURNING *
		)
		SELECT ` + productColumns + `
		FROM p JOIN categories c ON c.id = p.category_id`

	d, err := scanProduct(r.q.QueryRowContext(ctx, query,
		id, p.SKU, p.Name, p.Description, p.Price, p.Stock, p.CategoryID, p.IsActive))
	if err != nil {
		return nil, translateProductErr(err)
	}
	return d, nil
}

func (r *ProductRepository) GetByID(ctx context.Context, id string) (*model.ProductDetail, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	const query = `
		SELECT ` + productColumns + `
		FROM products p JOIN categories c ON c.id = p.category_id
		WHERE p.id = $1 AND p.deleted_at IS NULL`

	d, err := scanProduct(r.q.QueryRowContext(ctx, query, id))
	if err != nil {
		return nil, translateProductErr(err)
	}
	return d, nil
}

 // LockProduct mengunci baris produk selama transaksi berlangsung.
func (r *ProductRepository) LockProduct(ctx context.Context, id string) error {
    ctx, cancel := r.withTimeout(ctx)
    defer cancel()

    var productID string
    err := r.q.QueryRowContext(ctx,
        `SELECT id
         FROM products
         WHERE id = $1 AND deleted_at IS NULL
         FOR UPDATE`,
        id,
    ).Scan(&productID)

    if err != nil {
        return translateProductErr(err)
    }

    return nil
}


func (r *ProductRepository) List(ctx context.Context, f model.ProductFilter) ([]model.ProductDetail, int, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	where := []string{"p.deleted_at IS NULL"}
	var args []any

	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, strings.Replace(cond, "?", fmt.Sprintf("$%d", len(args)), 1))
	}

	if f.Search != "" {
		add(`p.name ILIKE '%' || ?::text || '%' ESCAPE '\'`, escapeLike(f.Search))
	}
	if f.CategoryID != nil {
		add(`p.category_id = ?::uuid`, *f.CategoryID)
	}
	if f.MinPrice != nil {
		add(`p.price >= ?::numeric`, *f.MinPrice)
	}
	if f.MaxPrice != nil {
		add(`p.price <= ?::numeric`, *f.MaxPrice)
	}
	if f.IsActive != nil {
		add(`p.is_active = ?::boolean`, *f.IsActive)
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM products p WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortCol, ok := sortColumns[f.Sort]
	if !ok {
		sortCol = sortColumns["created_at"]
	}

	direction := "DESC"
	if f.Order == "asc" {
		direction = "ASC"
	}

	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	listSQL := fmt.Sprintf(`
		SELECT %s
		FROM products p JOIN categories c ON c.id = p.category_id
		WHERE %s
		ORDER BY %s %s, p.id
		LIMIT $%d OFFSET $%d`, productColumns, whereSQL, sortCol, direction, len(args)-1, len(args))

	rows, err := r.q.QueryContext(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.ProductDetail, 0, f.Limit)
	for rows.Next() {
		d, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *d)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *ProductRepository) SoftDelete(ctx context.Context, id string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	res, err := r.q.ExecContext(ctx,
		`UPDATE products SET deleted_at = now(), updated_at = now()
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

func (r *ProductRepository) WithTx(ctx context.Context, fn func(tx service.ProductRepository) error) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	txRepo := &ProductRepository{db: r.db, q: tx, timeout: r.timeout}
	if err := fn(txRepo); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// LockCategory mengunci baris kategori aktif dengan FOR SHARE.
func (r *ProductRepository) LockCategory(ctx context.Context, categoryID string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var got string
	err := r.q.QueryRowContext(ctx,
		`SELECT id FROM categories WHERE id = $1 AND deleted_at IS NULL FOR SHARE`, categoryID).Scan(&got)

	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrCategoryNotFound
	}
	return err
}

func scanProduct(s scanner) (*model.ProductDetail, error) {
	var (
		d    model.ProductDetail
		desc sql.NullString
	)

	err := s.Scan(&d.ID, &d.SKU, &d.Name, &desc, &d.Price, &d.Stock, &d.IsActive,
		&d.CategoryID, &d.CategoryName, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}

	d.Description = nullToPtr(desc)
	return &d, nil
}

func translateProductErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrRecordNotFound
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch {
		case pqErr.Code == pgUniqueViolation && pqErr.Constraint == skuIndexName:
			return model.ErrDuplicateSKU
		case pqErr.Code == pgForeignKeyViolation:
			return model.ErrCategoryNotFound
		}
	}
	return err
}
