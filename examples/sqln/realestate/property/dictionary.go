package property

import (
	"context"

	"github.com/joaoprofile/gofi-sdk-go/sqln"
)

// Dictionary holds the reference data the interpreter recognizes: names
// typed in a search become filters on these rows.
type Dictionary struct {
	Types         []Named        `json:"types"`
	Offers        []Named        `json:"offers"`
	Conditions    []Named        `json:"conditions"`
	OwnerTypes    []Named        `json:"ownerTypes"`
	Features      []Named        `json:"features"`
	States        []Named        `json:"states"`
	Cities        []City         `json:"cities"`
	Neighborhoods []Neighborhood `json:"neighborhoods"`
	Owners        []Owner        `json:"owners"`
}

// Named is a coded row: property type, offer, condition, owner type,
// feature or state (code = UF).
type Named struct {
	Code string `db:"code" json:"code"`
	Name string `db:"name" json:"name"`
}

type City struct {
	ID   int64  `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
	UF   string `db:"uf" json:"uf"`
}

type Neighborhood struct {
	ID     int64  `db:"id" json:"id"`
	Name   string `db:"name" json:"name"`
	CityID int64  `db:"city_id" json:"cityId"`
	City   string `db:"city" json:"city"`
	UF     string `db:"uf" json:"uf"`
}

type Owner struct {
	ID   int64  `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
	Type string `db:"owner_type" json:"type"`
}

// LoadDictionary reads the reference data; it is small and loaded once.
func LoadDictionary(ctx context.Context) (*Dictionary, error) {
	var d Dictionary
	var err error
	named := func(table string) []Named {
		if err != nil {
			return nil
		}
		var rows []Named
		// table is one of the constants below, never input.
		rows, err = sqln.Find[Named](ctx, "SELECT code, name FROM "+table+" ORDER BY name").List()
		return rows
	}
	d.Types = named("property_types")
	d.Offers = named("offer_types")
	d.Conditions = named("conditions")
	d.OwnerTypes = named("owner_types")
	d.Features = named("features")
	if err != nil {
		return nil, err
	}
	if d.States, err = sqln.Find[Named](ctx, "SELECT uf AS code, name FROM states ORDER BY uf").List(); err != nil {
		return nil, err
	}
	if d.Cities, err = sqln.Find[City](ctx, "SELECT id, name, uf FROM cities ORDER BY name").List(); err != nil {
		return nil, err
	}
	if d.Neighborhoods, err = sqln.Find[Neighborhood](ctx,
		`SELECT n.id, n.name, n.city_id, c.name AS city, c.uf FROM neighborhoods n
		 JOIN cities c ON c.id = n.city_id ORDER BY c.name, n.name`).List(); err != nil {
		return nil, err
	}
	if d.Owners, err = sqln.Find[Owner](ctx, "SELECT id, name, owner_type FROM owners ORDER BY name").List(); err != nil {
		return nil, err
	}
	return &d, nil
}
