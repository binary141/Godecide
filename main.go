// main is main
package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	feel "github.com/superisaac/FEEL.go"
)

// Definitions is the root element of a DMN file
type Definitions struct {
	XMLName   xml.Name    `xml:"definitions"`
	ID        string      `xml:"id,attr"`
	Name      string      `xml:"name,attr"`
	Namespace string      `xml:"namespace,attr"`
	Decisions []Decision  `xml:"decision"`
	InputData []InputData `xml:"inputData"`
	Version   string
}

// Decision represents a single decision node in the DRG
type Decision struct {
	ID                      string                   `xml:"id,attr"`
	Name                    string                   `xml:"name,attr"`
	Variable                Variable                 `xml:"variable"`
	InformationRequirements []InformationRequirement `xml:"informationRequirement"`
	LiteralExpression       *LiteralExpression       `xml:"literalExpression"`
}

// Variable describes the output variable of a decision or input data
type Variable struct {
	Name    string `xml:"name,attr"`
	TypeRef string `xml:"typeRef,attr"`
}

// InformationRequirement is an edge in the DRG pointing to a required input or decision
type InformationRequirement struct {
	ID               string            `xml:"id,attr"`
	RequiredInput    *RequiredInput    `xml:"requiredInput"`
	RequiredDecision *RequiredDecision `xml:"requiredDecision"`
}

// RequiredDecision holds the href reference to an inputData element
type RequiredDecision struct {
	Href string `xml:"href,attr"`
}

// ResolvedID strips the "#" prefix from the href to get the raw element ID
func (r RequiredDecision) ResolvedID() string {
	return strings.TrimPrefix(r.Href, "#")
}

// RequiredInput holds the href reference to an inputData element
type RequiredInput struct {
	Href string `xml:"href,attr"`
}

// ResolvedID strips the "#" prefix from the href to get the raw element ID
func (r RequiredInput) ResolvedID() string {
	return strings.TrimPrefix(r.Href, "#")
}

// LiteralExpression holds a single FEEL expression as text
type LiteralExpression struct {
	Text string `xml:"text"`
}

// InputData represents an external input to the decision graph
type InputData struct {
	ID       string   `xml:"id,attr"`
	Name     string   `xml:"name,attr"`
	Variable Variable `xml:"variable"`
}

var ErrMissingInput = errors.New("missing required input")

func (d Definitions) Evaluate(context map[string]any) (map[string]any, error) {
	if len(d.InputData) != 0 && len(context) == 0 {
		return nil, ErrMissingInput
	}

	// the string key is the variable name of the input
	inputMap := make(map[string]Variable, len(context))

	for _, i := range d.InputData {
		if _, found := context[i.Name]; !found {
			return nil, fmt.Errorf("%w: %s not found", ErrMissingInput, i.Name)
		}

		// todo make sure the types are the same

		inputMap[i.ID] = i.Variable
	}

	// gets re-used across all Decisions
	ctx := map[string]any{}
	decisionOutputs := map[string]any{}

	for _, d := range d.Decisions {
		log.Println(d)
		for _, i := range d.InformationRequirements {
			if i.RequiredInput == nil && i.RequiredDecision == nil {
				return nil, fmt.Errorf("information requirement: %s needs either an input or decision", i.ID)
			}

			if i.RequiredInput != nil && i.RequiredDecision != nil {
				return nil, fmt.Errorf("information requirement: %s can only have one input or decision", i.ID)
			}

			if i.RequiredInput != nil {
				variable, hasInput := inputMap[i.RequiredInput.ResolvedID()]
				if !hasInput {
					return nil, fmt.Errorf("%w: %s not found", ErrMissingInput, i.ID)
				}

				ctx[variable.Name] = context[variable.Name]
			}

			if i.RequiredDecision != nil {
				_, hasDecision := decisionOutputs[i.RequiredDecision.ResolvedID()]
				if !hasDecision {
					return nil, fmt.Errorf("%w: %s not found", ErrMissingInput, i.ID)
				}
			}
		}

		log.Println(ctx)

		if d.LiteralExpression != nil {
			ctxBytes, err := json.Marshal(ctx)
			if err != nil {
				return nil, err
			}

			ret, err := feel.EvalString(d.LiteralExpression.Text, string(ctxBytes))
			if err != nil {
				return nil, err
			}

			log.Println(ret)

			decisionOutputs[d.ID] = ret
			ctx[d.Variable.Name] = ret
			log.Printf("Setting output: %+v for key: %+v", ret, d.ID)
		}
	}

	log.Println(inputMap)

	// fmt.Println(d.Decisions[0].LiteralExpression.Text)

	return nil, nil
}

func Parse(data []byte) (Definitions, error) {
	var d Definitions

	err := xml.Unmarshal(data, &d)
	if err != nil {
		fmt.Printf("Unable to unmarshal data: %+v", err)
		return d, err
	}

	v, err := DetectVersion(d.XMLName.Space)
	if err != nil {
		fmt.Printf("Unable to detect version: %+v", err)
		return d, err
	}

	d.Version = v

	return d, nil
}

func main() {
	_, err := os.Stat("./file2.dmn")
	if err != nil {
		log.Println("Couldn't find file.dmn")
		return
	}

	f, err := os.Open("./file2.dmn")
	if err != nil {
		log.Println("Couldn't open file.dmn")
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

	log.Printf("Definitions: %+v", d)

	inputs := map[string]any{
		"First Name": "myFirst",
		"Last Name":  "myLast",
		"Age":        69,
		"City":       "aCity",
	}

	evaluation, err := d.Evaluate(inputs)
	if err != nil {
		log.Printf("Couldn't evaluate: %v", err)
		return
	}

	fmt.Printf("Evaluated: %+v", evaluation)
}
