DROP TABLE IF EXISTS ftx_properties_search_lex;

DROP INDEX IF EXISTS ftx_properties_search;

ALTER TABLE properties DROP COLUMN IF EXISTS ftx_properties_search_tsv;
