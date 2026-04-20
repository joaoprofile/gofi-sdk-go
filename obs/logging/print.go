package logging

import (
	"encoding/json"
	"fmt"
)

// PrintStruct imprime a representação %+v de uma struct no stdout.
func PrintStruct(v interface{}) {
	fmt.Printf("%+v\n", v)
}

// PrintStructToJson imprime a representação JSON indentada de uma struct no stdout.
func PrintStructToJson(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}
