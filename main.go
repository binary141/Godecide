// main is main
//
//go:generate go run ./cmd/gentests
package main

import (
	"fmt"
	"io"
	"log"
	"os"
)

func main() {
	filename := "./out-of-order-2.dmn"

	if len(os.Args) == 2 {
		filename = os.Args[1]
	}

	_, err := os.Stat(filename)
	if err != nil {
		log.Println("Couldn't find " + filename)
		return
	}

	f, err := os.Open(filename)
	if err != nil {
		log.Println("Couldn't open " + filename)
		return
	}

	fileBytes, err := io.ReadAll(f)
	if err != nil {
		log.Printf("Couldn't read file bytes")
		return
	}

	d, err := Parse(fileBytes)
	if err != nil {
		log.Printf("Couldn't parse file bytes: %v", err)
		return
	}

	inputs := map[string]any{
		"First Name":        "Jane",
		"Last Name":         "Smith",
		"Department":        "Engineering",
		"Job Title":         "Engineer",
		"City":              "Austin",
		"Country":           "USA",
		"Company":           "Acme",
		"Team":              "Platform",
		"Employment Status": "\"EMPLOYED\"",
		"Age":               18,
		"RiskCategory":      "Medium",
		"isAffordable":      true,
		"loan":              map[string]any{"principal": float64(600000), "rate": float64(0.0375), "termMonths": float64(360)},
		"numList":           []any{},
		"list1":             []any{"a", "b", "c"},
		"list2":             []any{"x", "y", "z"},
		"string1":           "a",
		"num1":              1,
		"num2":              2,
		"num3":              3,
	}

	evaluation, err := d.Evaluate(inputs)
	if err != nil {
		log.Printf("Couldn't evaluate: %v", err)
		return
	}

	for k, v := range evaluation {
		fmt.Printf("%s -> %+v\n", k, v)
	}
}
