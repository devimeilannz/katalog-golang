-- gen_random_uuid() bawaan PostgreSQL 13+, tidak perlu extension.
CREATE TABLE categories (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL CHECK (char_length(name) BETWEEN 3 AND 100),
    description TEXT         CHECK (description IS NULL OR char_length(description) <= 500),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ  NULL
);

-- Nama unik hanya di antara kategori yang belum dihapus.
CREATE UNIQUE INDEX uq_categories_name_active
    ON categories (name)
    WHERE deleted_at IS NULL;
