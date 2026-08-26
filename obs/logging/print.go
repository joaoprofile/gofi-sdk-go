package logging

import (
	"encoding/json"
	"fmt"
)

// PrintStruct prints the %+v representation of a struct to stdout.
func PrintStruct(v interface{}) {
	fmt.Printf("%+v\n", v)
}

// PrintStructToJson prints the indented JSON representation of a struct to stdout.
func PrintStructToJson(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}
