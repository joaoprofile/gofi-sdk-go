package common

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"reflect"
)

type String sql.NullString

func (t *String) Scan(value any) error {
	var i sql.NullString
	if err := i.Scan(value); err != nil {
		return err
	}

	if reflect.TypeOf(value) == nil {
		*t = String{i.String, false}
	} else {
		*t = String{i.String, true}
	}

	return nil
}

func (s String) Value() (driver.Value, error) {
	if !s.Valid {
		return nil, nil
	}

	return s.String, nil
}

func (t String) MarshalJSON() ([]byte, error) {
	if !t.Valid {
		return json.Marshal(nil)
	}

	return json.Marshal(t.String)
}

func (t *String) UnmarshalJSON(data []byte) error {
	var ptr *string
	if err := json.Unmarshal(data, &ptr); err != nil {
		return err
	}

	if ptr != nil {
		t.Valid = true
		t.String = *ptr
	} else {
		t.Valid = false
	}

	return nil
}
