// main is main
//
//go:generate go run ./cmd/gentests
package main

import (
	"dmn/engine"
	"fmt"
	"io"
	"log"
	"os"
)

func main() {
	filename := "testdata/tck/TestCases/compliance-level-3/0003-iteration/0003-iteration.dmn"

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

	d, err := engine.Parse(fileBytes)
	if err != nil {
		log.Printf("Couldn't parse file bytes: %v", err)
		return
	}

	inputs := map[string]any{
		"Loans": []any{map[string]any{"amount": float64(200000), "rate": float64(.041), "term": float64(360)}, map[string]any{"amount": float64(20000), "rate": float64(.049), "term": float64(60)}},
	}

	evaluation, err := d.Evaluate(inputs)
	if err != nil {
		log.Printf("Couldn't evaluate: %v", err)
		return
	}

	for k, v := range evaluation {
		switch v := v.(type) {
		case []any:
			for _, v2 := range v {
				fmt.Println(v2)
			}
		default:
			break
		}

		fmt.Printf("%s -> %+v %T\n", k, v, v)
	}
}
