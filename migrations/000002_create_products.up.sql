CREATE TABLE products (
    id          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    sku         VARCHAR(50)   NOT NULL CHECK (sku ~ '^[A-Z0-9-]+$'),
    name        VARCHAR(150)  NOT NULL CHECK (char_length(name) BETWEEN 3 AND 150),
    description TEXT          CHECK (description IS NULL OR char_length(description) <= 1000),
    price       NUMERIC(15,2) NOT NULL CHECK (price > 0),
    stock       INTEGER       NOT NULL CHECK (stock >= 0),
    category_id UUID          NOT NULL REFERENCES categories (id),
    is_active   BOOLEAN       NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ   NULL
);

CREATE UNIQUE INDEX uq_products_sku_active ON products (sku) WHERE deleted_at IS NULL;
CREATE INDEX idx_products_category_id ON products (category_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_products_name        ON products (name)        WHERE deleted_at IS NULL;
CREATE INDEX idx_products_price       ON products (price)       WHERE deleted_at IS NULL;
CREATE INDEX idx_products_created_at  ON products (created_at DESC) WHERE deleted_at IS NULL; 
