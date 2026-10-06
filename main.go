// main is main
//
//go:generate go run ./cmd/gentests
package main

import (
	"dmn/engine"
	"encoding/json"
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <model.dmn> [inputs.json]\n", os.Args[0])
		os.Exit(2)
	}

	fileBytes, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatalf("Couldn't read %s: %v", os.Args[1], err)
	}

	d, err := engine.Parse(fileBytes)
	if err != nil {
		log.Fatalf("Couldn't parse file bytes: %v", err)
	}

	inputs := map[string]any{}
	if len(os.Args) == 3 {
		inputBytes, err := os.ReadFile(os.Args[2])
		if err != nil {
			log.Fatalf("Couldn't read %s: %v", os.Args[2], err)
		}
		if err := json.Unmarshal(inputBytes, &inputs); err != nil {
			log.Fatalf("Couldn't parse inputs: %v", err)
		}
	}

	evaluation, err := d.Evaluate(inputs)
	if err != nil {
		log.Fatalf("Couldn't evaluate: %v", err)
	}

	for k, v := range evaluation {
		fmt.Printf("%s -> %+v %T\n", k, v, v)
	}
}
