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
// web UI. Source, when non-empty, is the ID of another NodeSpec in the same
// GraphSpec whose output feeds this column instead of an external input -
// the column's Label/TypeRef are then derived from that node's output.
type InputSpec struct {
	Label   string `json:"label"`
	TypeRef string `json:"typeRef"`
	Source  string `json:"source"`
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

// NodeSpec is a single decision table node in the graph, as built in the
// web UI. ID is a client-assigned identifier (e.g. "n1") that doubles as
// the exported decision's ID, so evaluation results and requiredDecision
// edges can be keyed by it directly.
//
// The governance fields (Authority, DecisionMakers, DecisionOwners,
// ImpactedPerformanceIndicators) are read-only annotations, not evaluated:
// they reference IDs of entries in the graph's GovernanceSpec by client-
// assigned ID (e.g. "ks1", "ou1", "pi1"), the same way InputSpec.Source
// references another node.
type NodeSpec struct {
	ID                            string       `json:"id"`
	DecisionName                  string       `json:"decisionName"`
	HitPolicy                     string       `json:"hitPolicy"`
	Aggregation                   string       `json:"aggregation"`
	Inputs                        []InputSpec  `json:"inputs"`
	Outputs                       []OutputSpec `json:"outputs"`
	Rules                         []RuleSpec   `json:"rules"`
	Authority                     []string     `json:"authority"`
	DecisionMakers                []string     `json:"decisionMakers"`
	DecisionOwners                []string     `json:"decisionOwners"`
	ImpactedPerformanceIndicators []string     `json:"impactedPerformanceIndicators"`
}

// KnowledgeSourceSpec, OrganizationUnitSpec, and PerformanceIndicatorSpec are
// the governance/business-context DRG elements the builder UI can author,
// mirroring engine.KnowledgeSource, engine.OrganizationUnit, and
// engine.PerformanceIndicator but keyed by client-assigned ID so NodeSpec
// governance fields can reference them before they're exported to XML.
type KnowledgeSourceSpec struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type OrganizationUnitSpec struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PerformanceIndicatorSpec struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GovernanceSpec is the graph-wide pool of governance entities that nodes
// can link to. An organization unit's/performance indicator's inverse edges
// (decisionMade/decisionOwned/impactingDecision) aren't authored here - they
// are derived in buildDefinitions from which nodes reference them.
type GovernanceSpec struct {
	KnowledgeSources      []KnowledgeSourceSpec      `json:"knowledgeSources"`
	OrganizationUnits     []OrganizationUnitSpec     `json:"organizationUnits"`
	PerformanceIndicators []PerformanceIndicatorSpec `json:"performanceIndicators"`
}

// GraphSpec is the whole decision graph as built in the web UI: one or more
// decision table nodes, optionally wired to each other's outputs via
// InputSpec.Source, plus a shared pool of governance entities nodes can
// reference.
type GraphSpec struct {
	Nodes      []NodeSpec     `json:"nodes"`
	Governance GovernanceSpec `json:"governance"`
}

// exportDoc mirrors engine.Definitions but adds the xmlns attribute that
// encoding/xml doesn't emit automatically from XMLName.Space alone.
type exportDoc struct {
	XMLName               xml.Name                      `xml:"definitions"`
	Xmlns                 string                        `xml:"xmlns,attr"`
	ID                    string                        `xml:"id,attr"`
	Name                  string                        `xml:"name,attr"`
	Namespace             string                        `xml:"namespace,attr"`
	Decisions             []engine.Decision             `xml:"decision"`
	InputData             []engine.InputData            `xml:"inputData"`
	KnowledgeSources      []engine.KnowledgeSource      `xml:"knowledgeSource"`
	OrganizationUnits     []engine.OrganizationUnit     `xml:"organizationUnit"`
	PerformanceIndicators []engine.PerformanceIndicator `xml:"performanceIndicator"`
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

// buildDefinitions turns a GraphSpec into an engine.Definitions: one
// decision (with one decision table) per node, plus one inputData element
// per distinct external input label across the whole graph. A node's input
// column either binds to an external inputData (informationRequirement ->
// requiredInput) or to another node's output (informationRequirement ->
// requiredDecision, referencing that node's ID directly - node IDs are
// client-assigned and used as-is for the exported decision IDs, so
// evaluation results and graph edges can be keyed by them without a lookup
// table).
func buildDefinitions(spec GraphSpec) engine.Definitions {
	outputVar := make(map[string]string, len(spec.Nodes))
	outputType := make(map[string]string, len(spec.Nodes))
	for _, n := range spec.Nodes {
		v, t := n.DecisionName, ""
		if len(n.Outputs) == 1 {
			v, t = n.Outputs[0].Name, n.Outputs[0].TypeRef
		}
		outputVar[n.ID] = v
		outputType[n.ID] = t
	}

	var externalInputData []engine.InputData
	externalInputID := map[string]string{}
	ensureExternalInput := func(label, typeRef string) string {
		if id, ok := externalInputID[label]; ok {
			return id
		}
		id := fmt.Sprintf("i_%s_%d", slugify(label), len(externalInputData))
		externalInputID[label] = id
		externalInputData = append(externalInputData, engine.InputData{
			ID:       id,
			Name:     label,
			Variable: engine.Variable{Name: label, TypeRef: typeRef},
		})
		return id
	}

	// Governance entities are authored once in spec.Governance and linked to
	// by node ID; their inverse edges (decisionMade/decisionOwned/
	// impactingDecision) are derived here from which nodes reference them,
	// rather than authored redundantly on both sides.
	orgUnitDecisionsMade := map[string][]engine.DMNElementReference{}
	orgUnitDecisionsOwned := map[string][]engine.DMNElementReference{}
	piImpactingDecisions := map[string][]engine.DMNElementReference{}

	decisions := make([]engine.Decision, len(spec.Nodes))
	for ni, n := range spec.Nodes {
		var infoReqs []engine.InformationRequirement
		seenDecisionDep := map[string]bool{}
		dtInputs := make([]engine.Input, len(n.Inputs))
		effectiveType := make([]string, len(n.Inputs))

		for i, in := range n.Inputs {
			var exprText, label, typeRef string

			if in.Source != "" {
				exprText = outputVar[in.Source]
				label = exprText
				typeRef = outputType[in.Source]
				if typeRef == "" {
					typeRef = in.TypeRef
				}
				if !seenDecisionDep[in.Source] {
					seenDecisionDep[in.Source] = true
					infoReqs = append(infoReqs, engine.InformationRequirement{
						ID:               fmt.Sprintf("ir_%s_dec_%d", n.ID, len(infoReqs)),
						RequiredDecision: &engine.RequiredDecision{Href: "#" + in.Source},
					})
				}
			} else {
				exprText, label, typeRef = in.Label, in.Label, in.TypeRef
				id := ensureExternalInput(in.Label, in.TypeRef)
				infoReqs = append(infoReqs, engine.InformationRequirement{
					ID:            fmt.Sprintf("ir_%s_in_%d", n.ID, i),
					RequiredInput: &engine.RequiredInput{Href: "#" + id},
				})
			}

			effectiveType[i] = typeRef
			dtInputs[i] = engine.Input{
				ID:    fmt.Sprintf("in_%s_%d", n.ID, i),
				Label: label,
				InputExpression: engine.InputExpression{
					Text:    exprText,
					TypeRef: typeRef,
				},
			}
		}

		dtOutputs := make([]engine.Output, len(n.Outputs))
		for i, out := range n.Outputs {
			dtOutputs[i] = engine.Output{Name: out.Name}
		}

		rules := make([]engine.Rule, len(n.Rules))
		for ri, r := range n.Rules {
			inputEntries := make([]engine.InputEntry, len(n.Inputs))
			for i := range n.Inputs {
				raw := ""
				if i < len(r.InputEntries) {
					raw = r.InputEntries[i]
				}
				inputEntries[i] = engine.InputEntry{
					ID:   fmt.Sprintf("r%d_ie%d_%s", ri, i, n.ID),
					Text: normalizeInputEntry(raw, effectiveType[i]),
				}
			}

			outputEntries := make([]engine.OutputEntry, len(n.Outputs))
			for i := range n.Outputs {
				raw := ""
				if i < len(r.OutputEntries) {
					raw = r.OutputEntries[i]
				}
				outputEntries[i] = engine.OutputEntry{
					ID:   fmt.Sprintf("r%d_oe%d_%s", ri, i, n.ID),
					Text: normalizeOutputEntry(raw, n.Outputs[i].TypeRef),
				}
			}

			rules[ri] = engine.Rule{
				ID:            fmt.Sprintf("rule_%s_%d", n.ID, ri),
				InputEntries:  inputEntries,
				OutputEntries: outputEntries,
			}
		}

		var authReqs []engine.AuthorityRequirement
		for _, ksID := range n.Authority {
			authReqs = append(authReqs, engine.AuthorityRequirement{
				RequiredAuthority: &engine.RequiredAuthority{Href: "#" + ksID},
			})
		}

		var decisionMakers, decisionOwners, impactedPIs []engine.DMNElementReference
		for _, ouID := range n.DecisionMakers {
			decisionMakers = append(decisionMakers, engine.DMNElementReference{Href: "#" + ouID})
			orgUnitDecisionsMade[ouID] = append(orgUnitDecisionsMade[ouID], engine.DMNElementReference{Href: "#" + n.ID})
		}
		for _, ouID := range n.DecisionOwners {
			decisionOwners = append(decisionOwners, engine.DMNElementReference{Href: "#" + ouID})
			orgUnitDecisionsOwned[ouID] = append(orgUnitDecisionsOwned[ouID], engine.DMNElementReference{Href: "#" + n.ID})
		}
		for _, piID := range n.ImpactedPerformanceIndicators {
			impactedPIs = append(impactedPIs, engine.DMNElementReference{Href: "#" + piID})
			piImpactingDecisions[piID] = append(piImpactingDecisions[piID], engine.DMNElementReference{Href: "#" + n.ID})
		}

		decisions[ni] = engine.Decision{
			ID:   n.ID,
			Name: n.DecisionName,
			Variable: engine.Variable{
				Name:    outputVar[n.ID],
				TypeRef: outputType[n.ID],
			},
			InformationRequirements:       infoReqs,
			AuthorityRequirements:         authReqs,
			DecisionMakers:                decisionMakers,
			DecisionOwners:                decisionOwners,
			ImpactedPerformanceIndicators: impactedPIs,
			DecisionTables: []engine.DecisionTable{
				{
					HitPolicy:   n.HitPolicy,
					Aggregation: n.Aggregation,
					Inputs:      dtInputs,
					Output:      dtOutputs,
					Rules:       rules,
				},
			},
		}
	}

	knowledgeSources := make([]engine.KnowledgeSource, len(spec.Governance.KnowledgeSources))
	for i, ks := range spec.Governance.KnowledgeSources {
		knowledgeSources[i] = engine.KnowledgeSource{ID: ks.ID, Name: ks.Name, Type: ks.Type}
	}

	organizationUnits := make([]engine.OrganizationUnit, len(spec.Governance.OrganizationUnits))
	for i, ou := range spec.Governance.OrganizationUnits {
		organizationUnits[i] = engine.OrganizationUnit{
			ID:             ou.ID,
			Name:           ou.Name,
			DecisionsMade:  orgUnitDecisionsMade[ou.ID],
			DecisionsOwned: orgUnitDecisionsOwned[ou.ID],
		}
	}

	performanceIndicators := make([]engine.PerformanceIndicator, len(spec.Governance.PerformanceIndicators))
	for i, pi := range spec.Governance.PerformanceIndicators {
		performanceIndicators[i] = engine.PerformanceIndicator{
			ID:                 pi.ID,
			Name:               pi.Name,
			ImpactingDecisions: piImpactingDecisions[pi.ID],
		}
	}

	name := "Decision Graph"
	if len(spec.Nodes) > 0 {
		name = spec.Nodes[0].DecisionName
	}

	return engine.Definitions{
		ID:                    "_" + slugify(name),
		Name:                  name,
		Namespace:             "https://dmn-builder.local/" + slugify(name),
		Decisions:             decisions,
		InputData:             externalInputData,
		KnowledgeSources:      knowledgeSources,
		OrganizationUnits:     organizationUnits,
		PerformanceIndicators: performanceIndicators,
	}
}

func toXML(d engine.Definitions) ([]byte, error) {
	doc := exportDoc{
		XMLName:               xml.Name{Local: "definitions"},
		Xmlns:                 dmnNamespace,
		ID:                    d.ID,
		Name:                  d.Name,
		Namespace:             d.Namespace,
		Decisions:             d.Decisions,
		InputData:             d.InputData,
		KnowledgeSources:      d.KnowledgeSources,
		OrganizationUnits:     d.OrganizationUnits,
		PerformanceIndicators: d.PerformanceIndicators,
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}

	header := []byte(xml.Header)
	return append(header, body...), nil
}
