package property

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/joaoprofile/gofi-sdk-go/sqln"
)

// NewProperty is a listing sent to POST /api/properties; the codes and ids
// are those of GET /api/dictionary.
type NewProperty struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	PropertyType   string   `json:"propertyType"`
	OfferType      string   `json:"offerType"`
	Condition      string   `json:"condition"`
	OwnerID        int64    `json:"ownerId"`
	NeighborhoodID int64    `json:"neighborhoodId"`
	Street         string   `json:"street"`
	Bedrooms       int      `json:"bedrooms"`
	Suites         int      `json:"suites"`
	Bathrooms      int      `json:"bathrooms"`
	ParkingSpaces  int      `json:"parkingSpaces"`
	AreaM2         float64  `json:"areaM2"`
	Price          float64  `json:"price"`
	Features       []string `json:"features"`
}

// ErrInvalidProperty reports a listing with missing or inconsistent fields.
var ErrInvalidProperty = errors.New("imóvel inválido")

func (p NewProperty) validate() error {
	var problems []string
	check := func(ok bool, msg string) {
		if !ok {
			problems = append(problems, msg)
		}
	}
	check(strings.TrimSpace(p.Title) != "" && utf8.RuneCountInString(p.Title) <= 200, "título obrigatório, até 200 caracteres")
	check(strings.TrimSpace(p.Description) != "" && utf8.RuneCountInString(p.Description) <= 5000, "descrição obrigatória, até 5000 caracteres")
	check(p.PropertyType != "" && p.OfferType != "" && p.Condition != "", "tipo, oferta e condição obrigatórios")
	check(p.OwnerID > 0 && p.NeighborhoodID > 0, "anunciante e bairro obrigatórios")
	check(p.Bedrooms >= 0 && p.Suites >= 0 && p.Suites <= p.Bedrooms, "suítes não podem passar dos quartos")
	check(p.Bathrooms >= 0 && p.ParkingSpaces >= 0 && p.AreaM2 > 0 && p.Price > 0, "área e preço positivos")
	check(len(p.Features) <= 30, "no máximo 30 características")
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidProperty, strings.Join(problems, "; "))
	}
	return nil
}

const insertProperty = `INSERT INTO properties (title, description, property_type, offer_type, condition,
	owner_id, neighborhood_id, street, bedrooms, suites, bathrooms, parking_spaces, area_m2, price)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`

// refreshSearchText copies into the property the text of its related rows,
// which the full-text index reads (the seed migration runs the same).
const refreshSearchText = `UPDATE properties p SET
    location_text = concat_ws(' ', n.name, c.name, s.name, s.uf),
    facets_text   = concat_ws(' ', t.name, o.name, cd.name, ot.name),
    features_text = COALESCE((SELECT string_agg(f.name, ' ' ORDER BY f.name)
                              FROM property_features pf JOIN features f ON f.code = pf.feature
                              WHERE pf.property_id = p.id), '')
FROM neighborhoods n, cities c, states s, property_types t, offer_types o, conditions cd, owners ow, owner_types ot
WHERE n.id = p.neighborhood_id AND c.id = n.city_id AND s.uf = c.uf
  AND t.code = p.property_type AND o.code = p.offer_type AND cd.code = p.condition
  AND ow.id = p.owner_id AND ot.code = ow.owner_type
  AND p.id = $1`

// Create lists a property: the row, its features and its searched text in
// one transaction, then the lexicon. Unknown codes or ids fail the foreign
// keys (connection.IsForeignKeyViolation).
func (s *Service) Create(ctx context.Context, p NewProperty) (int64, error) {
	if err := p.validate(); err != nil {
		return 0, err
	}
	var id int64
	err := sqln.NewTransaction().Execute(ctx, func(ctx context.Context) error {
		stmt := sqln.NewStatement()
		row, err := stmt.QueryRow(ctx, insertProperty, p.Title, p.Description, p.PropertyType, p.OfferType,
			p.Condition, p.OwnerID, p.NeighborhoodID, p.Street, p.Bedrooms, p.Suites, p.Bathrooms,
			p.ParkingSpaces, p.AreaM2, p.Price)
		if err != nil {
			return err
		}
		if err := row.Scan(&id); err != nil {
			return err
		}
		for _, f := range p.Features {
			if _, err := stmt.Execute(ctx, "INSERT INTO property_features (property_id, feature) VALUES ($1, $2) ON CONFLICT DO NOTHING", id, f); err != nil {
				return err
			}
		}
		_, err = stmt.Execute(ctx, refreshSearchText, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, s.refreshLexicon(ctx)
}
