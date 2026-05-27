// main is main
//
//go:generate go run ./cmd/gentests
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
	"strconv"
	"strings"

	feel "github.com/superisaac/FEEL.go"
)

// Definitions is the root element of a DMN file
type Definitions struct {
	XMLName        xml.Name         `xml:"definitions"`
	ID             string           `xml:"id,attr"`
	Name           string           `xml:"name,attr"`
	Namespace      string           `xml:"namespace,attr"`
	Decisions      []Decision       `xml:"decision"`
	InputData      []InputData      `xml:"inputData"`
	ItemDefinition []ItemDefinition `xml:"itemDefinition"`
	Version        string
}

type ItemDefinition struct {
	Name          string           `xml:"name,attr"`
	IsCollection  string           `xml:"isCollection,attr"`
	TypeRef       string           `xml:"typeRef"`
	AllowedValues *AllowedValues   `xml:"allowedValues"`
	ItemComponent []ItemDefinition `xml:"itemComponent"`
}

type AllowedValues struct {
	Text string `xml:"text"`
}

// Decision represents a single decision node in the DRG
type Decision struct {
	ID                      string                   `xml:"id,attr"`
	Name                    string                   `xml:"name,attr"`
	Variable                Variable                 `xml:"variable"`
	InformationRequirements []InformationRequirement `xml:"informationRequirement"`
	DecisionTables          []DecisionTable          `xml:"decisionTable"`
	LiteralExpression       *LiteralExpression       `xml:"literalExpression"`
}

type DecisionTable struct {
	HitPolicy            string   `xml:"hitPolicy,attr"`
	OutputLabel          string   `xml:"outputLabel,attr"`
	PreferredOrientation string   `xml:"preferredOrientation,attr"`
	Inputs               []Input  `xml:"input"`
	Output               []Output `xml:"output"`
	Rules                []Rule   `xml:"rule"`
}

type Output struct {
	OutputValues OutputValues `xml:"outputValues"`
}

type OutputValues struct {
	Text string `xml:"text"`
}

type Input struct {
	ID              string          `xml:"id,attr"`
	Label           string          `xml:"label,attr"`
	InputExpression InputExpression `xml:"inputExpression"`
	InputValues     InputValues     `xml:"inputValues"`
}

type InputValues struct {
	Text string `xml:"text"`
}

type InputExpression struct {
	Text    string `xml:"text"`
	TypeRef string `xml:"typeRef,attr"`
}

type Rule struct {
	ID            string        `xml:"id,attr"`
	InputEntries  []InputEntry  `xml:"inputEntry"`
	OutputEntries []OutputEntry `xml:"outputEntry"`
}

type OutputEntry struct {
	Text string `xml:"text"`
	ID   string `xml:"id,attr"`
}

type InputEntry struct {
	ID   string `xml:"id,attr"`
	Text string `xml:"text"`
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
var ErrMisMatchTypes = errors.New("mismatch types")

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

	leafNodes := map[string]node{}

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
			leafNodes[d.ID] = n
		}
	}

	dfsNodes := dfs(nodes, edges)

	slices.SortFunc(dfsNodes, func(a, b node) int {
		return b.Post - a.Post
	})

	decisionNodes := []Decision{}

	for _, v := range leafNodes {
		decisionNodes = append(decisionNodes, v.Decision)
	}

	for _, v := range dfsNodes {
		_, isLeaf := leafNodes[v.Decision.ID]
		if isLeaf {
			// node was already added elsewhere
			continue
		}

		decisionNodes = append(decisionNodes, v.Decision)
	}

	d.Decisions = decisionNodes
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

	itemDefinitionMap := make(map[string]ItemDefinition, 0)

	d.TopologicalSort()

	for _, v := range d.ItemDefinition {
		itemDefinitionMap[v.Name] = v
	}

	// the string key is the variable name of the input
	inputMap := make(map[string]Variable, len(context))

	for _, i := range d.InputData {
		if _, found := context[i.Name]; !found {
			return nil, fmt.Errorf("%w: %s not found for input: %+v", ErrMissingInput, i.Name, i.ID)
		}

		// todo make sure the types are the same between input data and the context

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
					return nil, fmt.Errorf("%w: %s not found for input: %+v", ErrMissingInput, i.RequiredInput.Href, i.ID)
				}

				ctxVar := context[variable.Name]

				itemDef, hasDefinition := itemDefinitionMap[variable.TypeRef]
				if hasDefinition {
					if itemDef.AllowedValues != nil {
						allowedList := strings.Split(itemDef.AllowedValues.Text, ",")

						for i, v := range allowedList {
							v, err := strconv.Unquote(v)
							if err != nil {
								log.Printf("unable to unquote '%s': %v\n", v, err)
							}

							allowedList[i] = v
						}

						// todo move this to be go logic
						allowedVars := map[string]any{
							"Allowed Var":  ctxVar,
							"Allowed Vars": allowedList,
						}

						ctxBytes, err := json.Marshal(allowedVars)
						if err != nil {
							return nil, err
						}

						ret, err := feel.EvalString("list contains(Allowed Vars, Allowed Var)", string(ctxBytes))
						if err != nil {
							log.Printf("err: %+v", err)
						}

						r, ok := ret.(bool)
						if !ok {
							return nil, fmt.Errorf("expected ret to be a bool, got: %+v", ret)
						}

						if !r {
							return nil, fmt.Errorf("expected input: %v to be one of %v", ctxVar, allowedList)
						}

					}
				}

				ctx[variable.Name] = context[variable.Name]
			}

			if i.RequiredDecision != nil {
				_, hasDecision := decisionOutputs[i.RequiredDecision.ResolvedID()]
				if !hasDecision {
					return nil, fmt.Errorf("%w: %s not found for decision: %v", ErrMissingInput, i.ID, d.ID)
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

			feelNum, isNum := ret.(*feel.Number)
			if isNum {
				decisionOutputs[d.ID] = feelNum.Float64()
				ctx[d.Variable.Name] = feelNum.Float64()
				continue
			}

			decisionOutputs[d.ID] = ret
			ctx[d.Variable.Name] = ret
		}

		if len(d.DecisionTables) != 0 {
			for _, dt := range d.DecisionTables {
				if !IsValidHitPolicy(dt.HitPolicy) {
					return nil, fmt.Errorf("hit policy %s is not valid", dt.HitPolicy)
				}

				// for _, input := range dt.Input {
				// 	log.Println(input.InputExpression)
				// 	log.Println(input)
				// }

				// todo find better type?
				hits := map[string]any{}

				for _, rule := range dt.Rules {
					// todo make sure the types are the same from the ctx input to the rule input
					hit := true
					for j, ie := range rule.InputEntries {
						if ie.Text == "-" {
							continue
						}

						input := dt.Inputs[j]

						expression := ie.Text

						switch input.InputExpression.TypeRef {
						case "number":
							expression = fmt.Sprintf("%s %s", input.InputExpression.Text, ie.Text)
						case "string":
							// todo make sure this is right
							expression = fmt.Sprintf("list contains([%s], %s)", ie.Text, input.InputExpression.Text)
						}

						ctxBytes, err := json.Marshal(ctx)
						if err != nil {
							return nil, fmt.Errorf("unable to marshal ctx in rule evaluation: %w", err)
						}

						ret, err := feel.EvalString(expression, string(ctxBytes))
						if err != nil {
							log.Printf("err: %+v", err)
						}

						r, ok := ret.(bool)
						if !ok {
							return nil, fmt.Errorf("expected ret to be a bool, got: %+v", ret)
						}

						if hit {
							hit = r
						}
					}

					if hit {
						for _, oe := range rule.OutputEntries {
							var hitsList []any

							hitsAny, hasEntry := hits[d.ID]
							if !hasEntry {
								hitsList = make([]any, 0)
							} else {
								hitsList, _ = hitsAny.([]any)
							}

							oe.Text, _ = strconv.Unquote(oe.Text)

							hitsList = append(hitsList, oe.Text)

							hits[d.ID] = hitsList
						}
					}
				}

				hitsList, isList := hits[d.ID].([]any)
				if isList {
					switch dt.HitPolicy {
					case HitPolicyUnique:
						if len(hitsList) > 1 {
							return nil, fmt.Errorf("decision table had more than one output for unique policy: %+v", hitsList)
						}
					case HitPolicyPriority:
						outputs := strings.Split(dt.Output[0].OutputValues.Text, ",")

						for _, output := range outputs {
							output, _ = strconv.Unquote(output)
							for _, hit := range hitsList {
								if output == hit {
									hits[d.ID] = output
									return hits, nil
								}
							}
						}

					}

					if len(hitsList) == 1 {
						hits[d.ID] = hitsList[0]
					}
				}

				return hits, nil
			}
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
