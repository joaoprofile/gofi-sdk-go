CREATE TABLE categories (
    id   BIGSERIAL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE products (
    id          BIGSERIAL PRIMARY KEY,
    category_id BIGINT         NOT NULL REFERENCES categories (id),
    name        TEXT           NOT NULL,
    price       NUMERIC(10, 2) NOT NULL,
    active      BOOLEAN        NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ    NOT NULL
);

INSERT INTO categories (slug, name) VALUES
    ('electronics', 'Electronics'),
    ('books', 'Books'),
    ('office', 'Office');

INSERT INTO products (category_id, name, price, active, created_at) VALUES
    (1, 'Mechanical Keyboard', 349.90, TRUE, '2026-01-10T10:00:00Z'),
    (1, 'Wireless Mouse', 129.90, TRUE, '2026-02-03T10:00:00Z'),
    (1, 'USB-C Hub', 199.00, TRUE, '2026-03-15T10:00:00Z'),
    (1, '4K Monitor', 1899.00, TRUE, '2026-04-20T10:00:00Z'),
    (1, 'Noise Cancelling Headphones', 999.00, TRUE, '2026-05-05T10:00:00Z'),
    (1, 'Old Webcam', 89.00, FALSE, '2025-06-01T10:00:00Z'),
    (2, 'The Go Programming Language', 289.00, TRUE, '2026-01-22T10:00:00Z'),
    (2, 'Designing Data-Intensive Applications', 329.00, TRUE, '2026-02-18T10:00:00Z'),
    (2, 'Domain-Driven Design', 399.00, TRUE, '2026-03-02T10:00:00Z'),
    (2, 'Clean Architecture', 199.90, TRUE, '2026-06-11T10:00:00Z'),
    (3, 'Notebook A5', 29.90, TRUE, '2026-01-05T10:00:00Z'),
    (3, 'Gel Pen Pack', 19.90, TRUE, '2026-02-27T10:00:00Z'),
    (3, 'Desk Organizer', 79.00, TRUE, '2026-04-08T10:00:00Z'),
    (3, 'Ergonomic Chair', 1499.00, TRUE, '2026-07-19T10:00:00Z');
