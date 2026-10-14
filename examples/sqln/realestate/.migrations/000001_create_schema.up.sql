-- Reference data: the codes are the values the search interprets.
CREATE TABLE states (
    uf   CHAR(2) PRIMARY KEY,
    name TEXT    NOT NULL
);

CREATE TABLE cities (
    id   SERIAL PRIMARY KEY,
    name TEXT    NOT NULL,
    uf   CHAR(2) NOT NULL REFERENCES states (uf),
    UNIQUE (name, uf)
);

CREATE TABLE neighborhoods (
    id      SERIAL PRIMARY KEY,
    city_id INT  NOT NULL REFERENCES cities (id),
    name    TEXT NOT NULL,
    UNIQUE (city_id, name)
);

CREATE TABLE property_types (code TEXT PRIMARY KEY, name TEXT NOT NULL); -- casa, apartamento, flat…
CREATE TABLE offer_types    (code TEXT PRIMARY KEY, name TEXT NOT NULL); -- venda, aluguel, lancamento, leilao
CREATE TABLE conditions     (code TEXT PRIMARY KEY, name TEXT NOT NULL); -- novo, usado
CREATE TABLE owner_types    (code TEXT PRIMARY KEY, name TEXT NOT NULL); -- proprietario, corretor, imobiliaria
CREATE TABLE features       (code TEXT PRIMARY KEY, name TEXT NOT NULL); -- piscina, churrasqueira…

CREATE TABLE owners (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    owner_type TEXT NOT NULL REFERENCES owner_types (code),
    phone      TEXT NOT NULL
);

CREATE TABLE properties (
    id              SERIAL PRIMARY KEY,
    title           TEXT          NOT NULL,
    description     TEXT          NOT NULL,
    property_type   TEXT          NOT NULL REFERENCES property_types (code),
    offer_type      TEXT          NOT NULL REFERENCES offer_types (code),
    condition       TEXT          NOT NULL REFERENCES conditions (code),
    owner_id        INT           NOT NULL REFERENCES owners (id),
    neighborhood_id INT           NOT NULL REFERENCES neighborhoods (id),
    street          TEXT          NOT NULL DEFAULT '',
    bedrooms        SMALLINT      NOT NULL DEFAULT 0,
    suites          SMALLINT      NOT NULL DEFAULT 0,
    bathrooms       SMALLINT      NOT NULL DEFAULT 0,
    parking_spaces  SMALLINT      NOT NULL DEFAULT 0,
    area_m2         NUMERIC(10,2) NOT NULL,
    price           NUMERIC(14,2) NOT NULL,
    active          BOOLEAN       NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    -- Searched text from the related tables (full-text indexes read one
    -- table): rewritten by the repository whenever the property changes.
    location_text   TEXT          NOT NULL DEFAULT '',
    facets_text     TEXT          NOT NULL DEFAULT '',
    features_text   TEXT          NOT NULL DEFAULT ''
);

CREATE TABLE property_features (
    property_id INT  NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    feature     TEXT NOT NULL REFERENCES features (code),
    PRIMARY KEY (property_id, feature)
);

CREATE INDEX properties_neighborhood ON properties (neighborhood_id);
CREATE INDEX properties_facets ON properties (property_type, offer_type, condition);
