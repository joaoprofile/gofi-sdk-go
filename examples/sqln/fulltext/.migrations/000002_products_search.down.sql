DROP INDEX IF EXISTS ftx_products_search;

ALTER TABLE products DROP COLUMN IF EXISTS ftx_products_search_tsv;
