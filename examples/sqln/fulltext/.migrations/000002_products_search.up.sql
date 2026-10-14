CREATE EXTENSION IF NOT EXISTS unaccent;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'gofi_portuguese_unaccent') THEN
    CREATE TEXT SEARCH CONFIGURATION gofi_portuguese_unaccent (COPY = portuguese);
    ALTER TEXT SEARCH CONFIGURATION gofi_portuguese_unaccent ALTER MAPPING FOR hword, hword_part, word WITH unaccent, portuguese_stem;
  END IF;
END $$;

ALTER TABLE products ADD COLUMN ftx_products_search_tsv tsvector GENERATED ALWAYS AS (setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(name, '')), 'A') || setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(description, '')), 'B')) STORED;

CREATE INDEX ftx_products_search ON products USING GIN (ftx_products_search_tsv);

COMMENT ON INDEX ftx_products_search IS 'gofi-fts:v1:4ebc944afdf3c518';
