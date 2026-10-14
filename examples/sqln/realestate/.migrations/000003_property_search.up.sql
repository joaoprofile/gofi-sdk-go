CREATE EXTENSION IF NOT EXISTS unaccent;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'gofi_portuguese_unaccent') THEN
    CREATE TEXT SEARCH CONFIGURATION gofi_portuguese_unaccent (COPY = portuguese);
    ALTER TEXT SEARCH CONFIGURATION gofi_portuguese_unaccent ALTER MAPPING FOR hword, hword_part, word WITH unaccent, portuguese_stem;
  END IF;
END $$;

ALTER TABLE properties ADD COLUMN ftx_properties_search_tsv tsvector GENERATED ALWAYS AS (setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(title, '')), 'A') || setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(location_text, '')), 'A') || setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(facets_text, '')), 'B') || setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(features_text, '')), 'B') || setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(description, '')), 'C')) STORED;

CREATE INDEX ftx_properties_search ON properties USING GIN (ftx_properties_search_tsv);

COMMENT ON INDEX ftx_properties_search IS 'gofi-fts:v1:7c8e429b9a2b3753';

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE ftx_properties_search_lex (term text PRIMARY KEY, word text NOT NULL, ndoc integer NOT NULL);

CREATE INDEX ftx_properties_search_lexg ON ftx_properties_search_lex USING GIN (term gin_trgm_ops);
