// Package main implements a small HTTP server backing the DMN table builder
// web UI: it turns a JSON table spec into an engine.Definitions, evaluates
// it against sample inputs, and marshals it to DMN XML for export.
package main

import (
	"encoding/xml"
	"fmt"
	"strings"

	"dmn/engine"
)

// dmnNamespace is the DMN 1.5 model namespace. It's newer than the
// engine's own MaxSupportedVersion guard requires, so any version at or
// above the guard is accepted; using it keeps exported files consistent
// with the TCK fixtures already in this repo (see file.dmn).
const dmnNamespace = "https://www.omg.org/spec/DMN/20230324/MODEL/"

// InputSpec is a single input column of a decision table, as sent by the
// web UI.
type InputSpec struct {
	Label   string `json:"label"`
	TypeRef string `json:"typeRef"`
}

// OutputSpec is a single output column of a decision table.
type OutputSpec struct {
	Name    string `json:"name"`
	TypeRef string `json:"typeRef"`
}

// RuleSpec is one row of a decision table: one cell per input column
// followed by one cell per output column, in raw (unnormalized) form as
// typed by the user.
type RuleSpec struct {
	InputEntries  []string `json:"inputEntries"`
	OutputEntries []string `json:"outputEntries"`
}

// TableSpec is the whole decision table as built in the web UI.
type TableSpec struct {
	DecisionName string       `json:"decisionName"`
	HitPolicy    string       `json:"hitPolicy"`
	Aggregation  string       `json:"aggregation"`
	Inputs       []InputSpec  `json:"inputs"`
	Outputs      []OutputSpec `json:"outputs"`
	Rules        []RuleSpec   `json:"rules"`
}

// exportDoc mirrors engine.Definitions but adds the xmlns attribute that
// encoding/xml doesn't emit automatically from XMLName.Space alone.
type exportDoc struct {
	XMLName   xml.Name           `xml:"definitions"`
	Xmlns     string             `xml:"xmlns,attr"`
	ID        string             `xml:"id,attr"`
	Name      string             `xml:"name,attr"`
	Namespace string             `xml:"namespace,attr"`
	Decisions []engine.Decision  `xml:"decision"`
	InputData []engine.InputData `xml:"inputData"`
}

func slugify(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "table"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// normalizeInputEntry turns a plain value typed into a table cell into a
// FEEL unary test compatible with the engine's decision table evaluator,
// which concatenates the input expression with the entry text for numbers
// and wraps the entry in list contains(...) for strings. Text that already
// looks like FEEL (an operator, a range, or a quoted string) is passed
// through unchanged so power users can still write raw FEEL.
func normalizeInputEntry(text, typeRef string) string {
	text = strings.TrimSpace(text)
	if text == "" || text == "-" {
		return "-"
	}

	switch typeRef {
	case "number":
		if looksLikeFEELOperator(text) {
			return text
		}
		return "= " + text
	case "string":
		if strings.HasPrefix(text, "\"") {
			return text
		}
		parts := strings.Split(text, ",")
		for i, p := range parts {
			p = strings.TrimSpace(p)
			parts[i] = fmt.Sprintf("%q", p)
		}
		return strings.Join(parts, ", ")
	default:
		return text
	}
}

// normalizeOutputEntry mirrors normalizeInputEntry for output cells: string
// outputs are auto-quoted unless already quoted, other types pass through.
func normalizeOutputEntry(text, typeRef string) string {
	text = strings.TrimSpace(text)
	if typeRef == "string" && text != "" && !strings.HasPrefix(text, "\"") {
		return fmt.Sprintf("%q", text)
	}
	return text
}

func looksLikeFEELOperator(text string) bool {
	for _, op := range []string{"<", ">", "=", "!", "[", "(", ".."} {
		if strings.Contains(text, op) {
			return true
		}
	}
	return false
}

// buildDefinitions turns a TableSpec into an engine.Definitions with one
// decision, one decision table, and one inputData node per input column
// (wired up via informationRequirement so the engine binds context values
// to them during evaluation).
func buildDefinitions(spec TableSpec) engine.Definitions {
	decisionID := "d_" + slugify(spec.DecisionName)

	inputData := make([]engine.InputData, len(spec.Inputs))
	infoReqs := make([]engine.InformationRequirement, len(spec.Inputs))
	dtInputs := make([]engine.Input, len(spec.Inputs))

	for i, in := range spec.Inputs {
		id := fmt.Sprintf("i_%s_%d", slugify(in.Label), i)

		inputData[i] = engine.InputData{
			ID:   id,
			Name: in.Label,
			Variable: engine.Variable{
				Name:    in.Label,
				TypeRef: in.TypeRef,
			},
		}

		infoReqs[i] = engine.InformationRequirement{
			ID:            fmt.Sprintf("ir_%d", i),
			RequiredInput: &engine.RequiredInput{Href: "#" + id},
		}

		dtInputs[i] = engine.Input{
			ID:    fmt.Sprintf("in_%d", i),
			Label: in.Label,
			InputExpression: engine.InputExpression{
				Text:    in.Label,
				TypeRef: in.TypeRef,
			},
		}
	}

	dtOutputs := make([]engine.Output, len(spec.Outputs))
	for i, out := range spec.Outputs {
		dtOutputs[i] = engine.Output{Name: out.Name}
	}

	rules := make([]engine.Rule, len(spec.Rules))
	for ri, r := range spec.Rules {
		inputEntries := make([]engine.InputEntry, len(spec.Inputs))
		for i := range spec.Inputs {
			raw := ""
			if i < len(r.InputEntries) {
				raw = r.InputEntries[i]
			}
			inputEntries[i] = engine.InputEntry{
				ID:   fmt.Sprintf("r%d_ie%d", ri, i),
				Text: normalizeInputEntry(raw, spec.Inputs[i].TypeRef),
			}
		}

		outputEntries := make([]engine.OutputEntry, len(spec.Outputs))
		for i := range spec.Outputs {
			raw := ""
			if i < len(r.OutputEntries) {
				raw = r.OutputEntries[i]
			}
			outputEntries[i] = engine.OutputEntry{
				ID:   fmt.Sprintf("r%d_oe%d", ri, i),
				Text: normalizeOutputEntry(raw, spec.Outputs[i].TypeRef),
			}
		}

		rules[ri] = engine.Rule{
			ID:            fmt.Sprintf("rule_%d", ri),
			InputEntries:  inputEntries,
			OutputEntries: outputEntries,
		}
	}

	decisionVarName := spec.DecisionName
	decisionVarType := ""
	if len(spec.Outputs) == 1 {
		decisionVarName = spec.Outputs[0].Name
		decisionVarType = spec.Outputs[0].TypeRef
	}

	decision := engine.Decision{
		ID:   decisionID,
		Name: spec.DecisionName,
		Variable: engine.Variable{
			Name:    decisionVarName,
			TypeRef: decisionVarType,
		},
		InformationRequirements: infoReqs,
		DecisionTables: []engine.DecisionTable{
			{
				HitPolicy:   spec.HitPolicy,
				Aggregation: spec.Aggregation,
				Inputs:      dtInputs,
				Output:      dtOutputs,
				Rules:       rules,
			},
		},
	}

	return engine.Definitions{
		ID:        "_" + slugify(spec.DecisionName),
		Name:      spec.DecisionName,
		Namespace: "https://dmn-builder.local/" + slugify(spec.DecisionName),
		Decisions: []engine.Decision{decision},
		InputData: inputData,
	}
}

func toXML(d engine.Definitions) ([]byte, error) {
	doc := exportDoc{
		XMLName:   xml.Name{Local: "definitions"},
		Xmlns:     dmnNamespace,
		ID:        d.ID,
		Name:      d.Name,
		Namespace: d.Namespace,
		Decisions: d.Decisions,
		InputData: d.InputData,
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}

	header := []byte(xml.Header)
	return append(header, body...), nil
}
