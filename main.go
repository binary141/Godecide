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
	"slices"
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

type node struct {
	Decision Decision
	Pre      int
	Post     int
	Visited  bool
}

type edge struct {
	From string
	To   string
}

func (d *Definitions) TopologicalSort() {
	nodes := map[string]node{}

	edges := map[string][]edge{}

	for _, d := range d.Decisions {
		n := node{
			Decision: d,
			Visited:  false,
		}

		nodes[d.ID] = n
	}

	leafDecisions := []Decision{}

	for _, d := range d.Decisions {
		hasDecision := false
		for _, i := range d.InformationRequirements {
			if i.RequiredDecision == nil {
				continue
			}

			sourceID := i.RequiredDecision.ResolvedID()

			e := edge{
				From: sourceID,
				To:   d.ID,
			}

			edges[sourceID] = append(edges[sourceID], e)

			hasDecision = true
		}

		if !hasDecision {
			n := nodes[d.ID]
			n.Visited = true // because it is a leaf

			nodes[d.ID] = n
			leafDecisions = append(leafDecisions, d)
		}
	}

	dfsNodes := dfs(nodes, edges)

	slices.SortFunc(dfsNodes, func(a, b node) int {
		return b.Post - a.Post
	})

	// log.Println(leafDecisions)

	decisionNodes := []Decision{}

	for _, ln := range leafDecisions {
		log.Println(ln.ID)
	}

	for _, v := range dfsNodes {
		log.Printf("ID: %s Pre: %d Post: %d", v.Decision.ID, v.Pre, v.Post)

		// log.Println(v.Decision.ID)
		decisionNodes = append(decisionNodes, v.Decision)
	}

	d.Decisions = append(leafDecisions, decisionNodes...)
}

var counter = 0

func explore(nodes map[string]node, edges map[string][]edge, k string) map[string]node {
	n := nodes[k]
	n.Visited = true
	n.Pre = counter
	counter++
	nodes[k] = n

	for _, e := range edges[k] {
		if !nodes[e.To].Visited {
			explore(nodes, edges, e.To)
		}
	}

	n = nodes[k]
	n.Post = counter
	counter++
	nodes[k] = n

	return nodes
}

func dfs(nodes map[string]node, edges map[string][]edge) []node {

	for k, n := range nodes {
		if n.Visited {
			continue
		}

		explore(nodes, edges, k)
	}

	nodeList := []node{}

	for _, v := range nodes {
		nodeList = append(nodeList, v)
	}

	return nodeList
}

func (d Definitions) Evaluate(context map[string]any) (map[string]any, error) {
	if len(d.InputData) != 0 && len(context) == 0 {
		return nil, ErrMissingInput
	}

	d.TopologicalSort()

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

		if d.LiteralExpression != nil {
			ctxBytes, err := json.Marshal(ctx)
			if err != nil {
				return nil, err
			}

			ret, err := feel.EvalString(d.LiteralExpression.Text, string(ctxBytes))
			if err != nil {
				return nil, err
			}

			log.Printf("Adding output for key: %+v", d.ID)
			decisionOutputs[d.ID] = ret
			ctx[d.Variable.Name] = ret
		}
	}

	return decisionOutputs, nil
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
	filename := "./out-of-order-2.dmn"
	_, err := os.Stat(filename)
	if err != nil {
		log.Println("Couldn't find file.dmn")
		return
	}

	f, err := os.Open(filename)
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

	inputs := map[string]any{
		"First Name": "Jane",
		"Last Name":  "Smith",
		"Department": "Engineering",
		"Job Title":  "Engineer",
		"City":       "Austin",
		"Country":    "USA",
		"Company":    "Acme",
		"Team":       "Platform",
	}

	evaluation, err := d.Evaluate(inputs)
	if err != nil {
		log.Printf("Couldn't evaluate: %v", err)
		return
	}

	fmt.Printf("Evaluated: %+v", evaluation)
}
