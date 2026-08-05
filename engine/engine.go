package engine

import (
	"dmn/versions"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	feel "github.com/binary141/FEEL.go"
)

// Definitions is the root element of a DMN file
type Definitions struct {
	XMLName                 xml.Name                 `xml:"definitions"`
	ID                      string                   `xml:"id,attr"`
	Name                    string                   `xml:"name,attr"`
	Namespace               string                   `xml:"namespace,attr"`
	Decisions               []Decision               `xml:"decision"`
	InputData               []InputData              `xml:"inputData"`
	ItemDefinition          []ItemDefinition         `xml:"itemDefinition"`
	BusinessKnowledgeModels []BusinessKnowledgeModel `xml:"businessKnowledgeModel"`
	DecisionServices        []DecisionService        `xml:"decisionService"`
	Version                 string
}

// DecisionService packages one or more decisions behind a callable interface:
// invoking it by name runs its outputDecision (and whatever that decision
// depends on) against fresh inputs bound from the call's positional
// arguments, independent of the enclosing evaluation's context.
type DecisionService struct {
	ID              string             `xml:"id,attr"`
	Name            string             `xml:"name,attr"`
	Variable        Variable           `xml:"variable"`
	OutputDecisions []RequiredDecision `xml:"outputDecision"`
	InputDecisions  []RequiredDecision `xml:"inputDecision"`
	InputData       []RequiredInput    `xml:"inputData"`
}

// BusinessKnowledgeModel represents a reusable function invoked from decision logic
type BusinessKnowledgeModel struct {
	ID                string            `xml:"id,attr"`
	Name              string            `xml:"name,attr"`
	Variable          Variable          `xml:"variable"`
	EncapsulatedLogic EncapsulatedLogic `xml:"encapsulatedLogic"`
}

// EncapsulatedLogic holds the parameters and expression body of a business knowledge model
type EncapsulatedLogic struct {
	FormalParameters   []FormalParameter   `xml:"formalParameter"`
	LiteralExpression  LiteralExpression   `xml:"literalExpression"`
	FunctionDefinition *FunctionDefinition `xml:"functionDefinition"`
}

// FormalParameter is a single named parameter of a business knowledge model
type FormalParameter struct {
	Name    string `xml:"name,attr"`
	TypeRef string `xml:"typeRef,attr"`
}

// FEELFunctionLiteral renders the business knowledge model as a FEEL function literal
// so it can be injected into the evaluation context and invoked by name.
func (b BusinessKnowledgeModel) FEELFunctionLiteral() string {
	params := make([]string, len(b.EncapsulatedLogic.FormalParameters))
	for i, p := range b.EncapsulatedLogic.FormalParameters {
		params[i] = p.Name
	}

	body := b.EncapsulatedLogic.LiteralExpression.Text
	if b.EncapsulatedLogic.FunctionDefinition != nil {
		body = b.EncapsulatedLogic.FunctionDefinition.FEELFunctionLiteral()
	}

	return fmt.Sprintf("function(%s) %s", strings.Join(params, ", "), body)
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
	KnowledgeRequirements   []KnowledgeRequirement   `xml:"knowledgeRequirement"`
	DecisionTables          []DecisionTable          `xml:"decisionTable"`
	LiteralExpression       *LiteralExpression       `xml:"literalExpression"`
	Context                 *Context                 `xml:"context"`
	FunctionDefinition      *FunctionDefinition      `xml:"functionDefinition"`
	Invocation              *Invocation              `xml:"invocation"`
	Relation                *Relation                `xml:"relation"`
	List                    *List                    `xml:"list"`
	Filter                  *Filter                  `xml:"filter"`
	For                     *For                     `xml:"for"`
	Conditional             *Conditional             `xml:"conditional"`
	Some                    *Quantified              `xml:"some"`
	Every                   *Quantified              `xml:"every"`
}

// Invocation represents a DMN <invocation> element: a call to a business
// knowledge model (referenced by name, not id) with named parameter
// bindings, e.g. "Some BKM"(Param One: expr1, Param Two: expr2).
type Invocation struct {
	LiteralExpression LiteralExpression `xml:"literalExpression"`
	Bindings          []Binding         `xml:"binding"`
}

// Binding is a single "parameter: expression" argument of an Invocation.
type Binding struct {
	Parameter         Parameter         `xml:"parameter"`
	LiteralExpression LiteralExpression `xml:"literalExpression"`
}

// Parameter names a single formal parameter bound by an Invocation.
type Parameter struct {
	Name string `xml:"name,attr"`
}

// FEELCallExpression renders the invocation as a FEEL named-argument function
// call so it can be evaluated like any other literal expression.
func (inv Invocation) FEELCallExpression() string {
	args := make([]string, len(inv.Bindings))
	for i, b := range inv.Bindings {
		args[i] = fmt.Sprintf("%s: %s", b.Parameter.Name, b.LiteralExpression.Text)
	}

	return fmt.Sprintf("%s(%s)", strings.TrimSpace(inv.LiteralExpression.Text), strings.Join(args, ", "))
}

// FunctionDefinition represents a DMN <functionDefinition> element: a
// decision (or BKM) body that evaluates to a callable FEEL function rather
// than a plain value.
type FunctionDefinition struct {
	FormalParameters   []FormalParameter   `xml:"formalParameter"`
	LiteralExpression  LiteralExpression   `xml:"literalExpression"`
	FunctionDefinition *FunctionDefinition `xml:"functionDefinition"`
}

// FEELFunctionLiteral renders the function definition as a FEEL function
// literal so it can be parsed and bound to the decision's variable. A
// functionDefinition's body is either a literal expression or another
// (nested) functionDefinition, e.g. for currying: function(a) function(b) ...
func (f FunctionDefinition) FEELFunctionLiteral() string {
	params := make([]string, len(f.FormalParameters))
	for i, p := range f.FormalParameters {
		params[i] = p.Name
	}

	body := f.LiteralExpression.Text
	if f.FunctionDefinition != nil {
		body = f.FunctionDefinition.FEELFunctionLiteral()
	}

	return fmt.Sprintf("function(%s) %s", strings.Join(params, ", "), body)
}

// Context represents a DMN <context> element: an ordered list of context
// entries, each binding a name to a value (or, if unnamed, providing the
// context's overall result).
type Context struct {
	Entries []ContextEntry `xml:"contextEntry"`
}

// ContextEntry is a single name/value binding within a Context. The value is
// either a literal FEEL expression or a nested context.
type ContextEntry struct {
	Variable          *Variable          `xml:"variable"`
	LiteralExpression *LiteralExpression `xml:"literalExpression"`
	Context           *Context           `xml:"context"`
	DecisionTable     *DecisionTable     `xml:"decisionTable"`
	Relation          *Relation          `xml:"relation"`
	List              *List              `xml:"list"`
	Filter            *Filter            `xml:"filter"`
	For               *For               `xml:"for"`
	Conditional       *Conditional       `xml:"conditional"`
	Some              *Quantified        `xml:"some"`
	Every             *Quantified        `xml:"every"`
}

// Expression is a generic holder for any of the DMN expression variants that
// belong to the "expression" substitution group. It's used wherever the DMN
// schema wraps a single child expression of unspecified kind, e.g. the
// <if>/<then>/<else> children of <conditional> or the <in>/<match> children
// of <filter>. Exactly one field is populated after unmarshaling.
type Expression struct {
	LiteralExpression  *LiteralExpression  `xml:"literalExpression"`
	Context            *Context            `xml:"context"`
	FunctionDefinition *FunctionDefinition `xml:"functionDefinition"`
	Invocation         *Invocation         `xml:"invocation"`
	DecisionTable      *DecisionTable      `xml:"decisionTable"`
	Relation           *Relation           `xml:"relation"`
	List               *List               `xml:"list"`
	Filter             *Filter             `xml:"filter"`
	For                *For                `xml:"for"`
	Conditional        *Conditional        `xml:"conditional"`
	Some               *Quantified         `xml:"some"`
	Every              *Quantified         `xml:"every"`
}

// InformationItemRef names a single column of a Relation.
type InformationItemRef struct {
	Name string `xml:"name,attr"`
}

// Relation represents a DMN <relation> boxed expression: a table of named
// columns and rows, where each row is a positional list of expressions (one
// per column). Evaluates to a list of contexts, one per row.
type Relation struct {
	Columns []InformationItemRef `xml:"column"`
	Rows    []List               `xml:"row"`
}

// List represents a DMN <list> boxed expression: an ordered sequence of
// expressions of any (possibly mixed) kind. Also reused for <relation> rows,
// which share the same tList schema shape.
type List struct {
	Expressions []Expression
}

// UnmarshalXML decodes a <list> (or <row>) element's children in document
// order, dispatching each child element to the matching Expression field by
// tag name. encoding/xml struct tags can't express "any element from this
// set, in any order, possibly repeated" directly, so this walks the token
// stream by hand.
func (l *List) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			expr, err := decodeExpressionElement(d, t)
			if err != nil {
				return err
			}
			l.Expressions = append(l.Expressions, expr)
		case xml.EndElement:
			if t == start.End() {
				return nil
			}
		}
	}
}

// decodeExpressionElement decodes a single start element into an Expression,
// dispatching by tag name to the matching variant.
func decodeExpressionElement(d *xml.Decoder, t xml.StartElement) (Expression, error) {
	var e Expression

	switch t.Name.Local {
	case "literalExpression":
		e.LiteralExpression = &LiteralExpression{}
		return e, d.DecodeElement(e.LiteralExpression, &t)
	case "context":
		e.Context = &Context{}
		return e, d.DecodeElement(e.Context, &t)
	case "functionDefinition":
		e.FunctionDefinition = &FunctionDefinition{}
		return e, d.DecodeElement(e.FunctionDefinition, &t)
	case "invocation":
		e.Invocation = &Invocation{}
		return e, d.DecodeElement(e.Invocation, &t)
	case "decisionTable":
		e.DecisionTable = &DecisionTable{}
		return e, d.DecodeElement(e.DecisionTable, &t)
	case "relation":
		e.Relation = &Relation{}
		return e, d.DecodeElement(e.Relation, &t)
	case "list":
		e.List = &List{}
		return e, d.DecodeElement(e.List, &t)
	case "filter":
		e.Filter = &Filter{}
		return e, d.DecodeElement(e.Filter, &t)
	case "for":
		e.For = &For{}
		return e, d.DecodeElement(e.For, &t)
	case "conditional":
		e.Conditional = &Conditional{}
		return e, d.DecodeElement(e.Conditional, &t)
	case "some":
		e.Some = &Quantified{}
		return e, d.DecodeElement(e.Some, &t)
	case "every":
		e.Every = &Quantified{}
		return e, d.DecodeElement(e.Every, &t)
	default:
		return e, d.Skip()
	}
}

// Filter represents a DMN <filter> boxed expression: list[condition]. The
// <match> expression is evaluated once per element of <in>, with the
// implicit variable "item" bound to the element under test.
type Filter struct {
	In    Expression `xml:"in"`
	Match Expression `xml:"match"`
}

// Iterator holds the fields shared by <for>, <some>, and <every>: the
// variable bound on each iteration and the list being iterated.
type Iterator struct {
	IteratorVariable string     `xml:"iteratorVariable,attr"`
	In               Expression `xml:"in"`
}

// For represents a DMN <for> boxed expression: for x in list return expr.
type For struct {
	Iterator
	Return Expression `xml:"return"`
}

// Quantified represents a DMN <some>/<every> boxed expression:
// some/every x in list satisfies cond.
type Quantified struct {
	Iterator
	Satisfies Expression `xml:"satisfies"`
}

// Conditional represents a DMN <conditional> boxed expression:
// if cond then a else b.
type Conditional struct {
	If   Expression `xml:"if"`
	Then Expression `xml:"then"`
	Else Expression `xml:"else"`
}

// KnowledgeRequirement is an edge in the DRG pointing to a required business knowledge model
type KnowledgeRequirement struct {
	ID                string             `xml:"id,attr"`
	RequiredKnowledge *RequiredKnowledge `xml:"requiredKnowledge"`
}

// RequiredKnowledge holds the href reference to a businessKnowledgeModel element
type RequiredKnowledge struct {
	Href string `xml:"href,attr"`
}

// ResolvedID returns the fragment of the href (the part after the last "#"),
// which is the raw element ID. Handles both local ("#_id") and fully-qualified
// ("http://.../ns#_id") hrefs.
func (r RequiredKnowledge) ResolvedID() string {
	return resolveHrefID(r.Href)
}

type DecisionTable struct {
	HitPolicy            string   `xml:"hitPolicy,attr"`
	Aggregation          string   `xml:"aggregation,attr"`
	OutputLabel          string   `xml:"outputLabel,attr"`
	PreferredOrientation string   `xml:"preferredOrientation,attr"`
	Inputs               []Input  `xml:"input"`
	Output               []Output `xml:"output"`
	Rules                []Rule   `xml:"rule"`
}

type Output struct {
	Name         string       `xml:"name,attr"`
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

// ResolvedID returns the fragment of the href (the part after the last "#"),
// which is the raw element ID. Handles both local ("#_id") and fully-qualified
// ("http://.../ns#_id") hrefs.
func (r RequiredDecision) ResolvedID() string {
	return resolveHrefID(r.Href)
}

// RequiredInput holds the href reference to an inputData element
type RequiredInput struct {
	Href string `xml:"href,attr"`
}

// ResolvedID returns the fragment of the href (the part after the last "#"),
// which is the raw element ID. Handles both local ("#_id") and fully-qualified
// ("http://.../ns#_id") hrefs.
func (r RequiredInput) ResolvedID() string {
	return resolveHrefID(r.Href)
}

// resolveHrefID extracts the element-ID fragment from a DMN href, which may
// be a bare local reference ("#_id") or a fully-qualified URL with a
// fragment ("http://.../namespace#_id").
func resolveHrefID(href string) string {
	if i := strings.LastIndex(href, "#"); i != -1 {
		return href[i+1:]
	}
	return href
}

// LiteralExpression holds a single FEEL expression as text
type LiteralExpression struct {
	Text    string `xml:"text"`
	TypeRef string `xml:"typeRef,attr"`
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

// toFEELValue recursively converts a plain Go value (as supplied by callers
// of Evaluate, e.g. int/float64/map[string]any/[]any from json.Unmarshal) into
// FEEL-native types. Previously this normalization happened implicitly by
// round-tripping ctx through json.Marshal and FEEL's own text parser; pushing
// ctx into the interpreter directly (see evalFEEL) skips that parser, so
// numeric/nil types need to be normalized by hand instead. Values already in
// FEEL-native form (e.g. *feel.Number results from earlier decisions) pass
// through unchanged.
func toFEELValue(v any) any {
	switch vv := v.(type) {
	case nil:
		return feel.Null
	case map[string]any:
		out := make(map[string]any, len(vv))
		for k, val := range vv {
			out[k] = toFEELValue(val)
		}
		return out
	case []any:
		out := make([]any, len(vv))
		for i, val := range vv {
			out[i] = toFEELValue(val)
		}
		return out
	case int, int64, float64:
		return feel.N(vv)
	default:
		return vv
	}
}

// evalFEEL evaluates a FEEL expression against ctx directly, without the
// json.Marshal + re-parse round trip that EvalString requires for its
// string-encoded scopes. That round trip re-serializes and re-tokenizes the
// full ctx on every call, so its cost grows with len(ctx); pushing ctx as a
// feel.Scope directly is O(1) regardless of how many entries it holds.
func evalFEEL(text string, ctx map[string]any, extraScope string, nativeScope map[string]any, itemDefinitionMap map[string]ItemDefinition) (any, error) {
	intp := feel.NewIntepreter()
	intp.Push(feel.Scope(ctx))
	intp.TypeResolver = func(name string) (string, bool) {
		return instanceOfTypeDescriptor(name, itemDefinitionMap)
	}

	if extraScope != "" {
		scopeAst, err := feel.ParseString(extraScope)
		if err != nil {
			return nil, err
		}

		r, err := scopeAst.Eval(intp)
		if err != nil {
			return nil, err
		}

		scope, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected scope to be a map, got: %+v", r)
		}

		intp.Push(scope)
	}

	if len(nativeScope) > 0 {
		intp.Push(feel.Scope(nativeScope))
	}

	ast, err := feel.ParseString(text)
	if err != nil {
		return nil, err
	}

	return ast.Eval(intp)
}

// decisionServiceFunc builds a callable FEEL value for a DecisionService:
// invoking it re-evaluates the whole document (root) against a fresh input
// set built from the call's positional arguments (bound to the service's
// declared inputData, in order), then returns its outputDecision's value —
// independent of whatever decision is invoking it.
func decisionServiceFunc(root Definitions, ds DecisionService, inputDataByID map[string]InputData) *feel.NativeFun {
	decisionByID := make(map[string]Decision, len(root.Decisions))
	for _, dec := range root.Decisions {
		decisionByID[dec.ID] = dec
	}

	// A decision service's parameters are its declared inputData (plain
	// external inputs) plus its inputDecision (another decision's output,
	// supplied directly instead of being computed) - in document order.
	inputNames := make([]string, 0, len(ds.InputData)+len(ds.InputDecisions))
	decisionParamIDs := make(map[string]string, len(ds.InputDecisions)) // param name -> decision ID
	for _, inp := range ds.InputData {
		inputNames = append(inputNames, inputDataByID[inp.ResolvedID()].Name)
	}
	for _, inp := range ds.InputDecisions {
		id := inp.ResolvedID()
		name := decisionByID[id].Variable.Name
		inputNames = append(inputNames, name)
		decisionParamIDs[name] = id
	}

	outputIDs := make([]string, len(ds.OutputDecisions))
	for i, od := range ds.OutputDecisions {
		outputIDs[i] = od.ResolvedID()
	}

	// Restrict the sub-evaluation to the outputDecision(s) and their
	// transitive decision dependencies. Evaluating the full document would
	// re-include whatever decision is invoking this service, which (since
	// that decision's own KnowledgeRequirement rebinds this very function)
	// recurses forever.
	included := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		if included[id] {
			return
		}
		dec, ok := decisionByID[id]
		if !ok {
			return
		}
		included[id] = true
		for _, ir := range dec.InformationRequirements {
			if ir.RequiredDecision != nil {
				visit(ir.RequiredDecision.ResolvedID())
			}
		}
	}
	for _, oid := range outputIDs {
		visit(oid)
	}

	subDecisions := make([]Decision, 0, len(included))
	for _, dec := range root.Decisions {
		if included[dec.ID] {
			subDecisions = append(subDecisions, dec)
		}
	}

	subDefs := root
	subDefs.Decisions = subDecisions

	fn := feel.NewNativeFunc(func(args map[string]any) (any, error) {
		subInputs := make(map[string]any, len(inputNames))
		var seedDecisions map[string]any
		for _, name := range inputNames {
			v, ok := args[name]
			if !ok {
				continue
			}
			if decID, isDecisionParam := decisionParamIDs[name]; isDecisionParam {
				if seedDecisions == nil {
					seedDecisions = map[string]any{}
				}
				seedDecisions[decID] = v
				continue
			}
			subInputs[name] = v
		}

		subResult, err := subDefs.evaluate(subInputs, seedDecisions)
		if err != nil {
			return nil, err
		}

		if len(outputIDs) == 1 {
			return subResult[outputIDs[0]], nil
		}

		combined := make(map[string]any, len(outputIDs))
		for _, oid := range outputIDs {
			combined[oid] = subResult[oid]
		}
		return combined, nil
	})

	return fn.Required(inputNames...)
}

// bkmFunc builds a callable FEEL value for a BusinessKnowledgeModel, so its
// formal-parameter and return-type coercion (declared on the encapsulated
// logic's literalExpression) can be enforced in Go rather than purely inside
// FEEL text: an argument that fails to conform to its declared typeRef makes
// the whole call null (the body is never evaluated), and a body result that
// fails to conform to the declared return typeRef becomes null.
func bkmFunc(bkm BusinessKnowledgeModel, itemDefinitionMap map[string]ItemDefinition) *feel.NativeFun {
	params := bkm.EncapsulatedLogic.FormalParameters
	paramNames := make([]string, len(params))
	for i, p := range params {
		paramNames[i] = p.Name
	}

	fn := feel.NewNativeFunc(func(args map[string]any) (any, error) {
		argCtx := make(map[string]any, len(params))
		for _, p := range params {
			v, ok := args[p.Name]
			if !ok {
				v = feel.Null
			}
			if p.TypeRef != "" {
				coerced := coerceToType(v, p.TypeRef, itemDefinitionMap)
				if isFEELNull(coerced) && !isFEELNull(v) {
					return feel.Null, nil
				}
				v = coerced
			}
			argCtx[p.Name] = v
		}

		if bkm.EncapsulatedLogic.FunctionDefinition != nil {
			// Curried BKM (a functionDefinition returning another
			// functionDefinition): return-type coercion isn't modeled for
			// this shape, evaluate the literal text as-is.
			return evalFEEL(bkm.EncapsulatedLogic.FunctionDefinition.FEELFunctionLiteral(), argCtx, "", nil, itemDefinitionMap)
		}

		body := bkm.EncapsulatedLogic.LiteralExpression
		ret, err := evalFEEL(body.Text, argCtx, "", nil, itemDefinitionMap)
		if err != nil {
			return nil, err
		}

		if body.TypeRef != "" {
			return coerceToType(ret, body.TypeRef, itemDefinitionMap), nil
		}
		return ret, nil
	})

	return fn.Required(paramNames...)
}

// evalContext evaluates a DMN <context> element against the given base
// scope. Entries are evaluated in document order, with each entry's value
// visible to the entries that follow it and to the final result. An entry
// with no name (a "result entry") becomes the context's overall value;
// otherwise the overall value is the map of all named entries.
// resolvePrimitiveType follows a chain of custom itemDefinition typeRefs
// (e.g. "tEligibility" -> "string") down to the underlying FEEL primitive
// name, so decision-table input matching can special-case "number"/"string"
// even when the input's declared type is a custom alias for one of them.
func resolvePrimitiveType(typeRef string, itemDefinitionMap map[string]ItemDefinition) string {
	seen := map[string]bool{}
	for {
		if seen[typeRef] {
			return typeRef
		}
		seen[typeRef] = true

		def, ok := itemDefinitionMap[typeRef]
		if !ok || def.TypeRef == "" {
			return typeRef
		}
		typeRef = def.TypeRef
	}
}

// instanceOfTypeDescriptor resolves a custom itemDefinition name into the
// structural type descriptor "instance of" understands: a builtin FEEL
// primitive name, or a "list<...>" / "context<field: type, ...>" descriptor
// built recursively from the itemDefinition's shape. ok is false for names
// that aren't a registered itemDefinition, leaving the type name to be
// checked as-is (e.g. a builtin like "number" or "context<...>" spelled
// directly in the FEEL text).
func instanceOfTypeDescriptor(name string, itemDefinitionMap map[string]ItemDefinition) (string, bool) {
	def, ok := itemDefinitionMap[name]
	if !ok {
		return "", false
	}
	return itemDefinitionDescriptor(def, itemDefinitionMap, map[string]bool{name: true}), true
}

// itemDefinitionDescriptor builds the structural descriptor for a resolved
// itemDefinition, following further custom typeRefs down to a primitive.
// seen guards against a typeRef cycle across itemDefinitions.
func itemDefinitionDescriptor(def ItemDefinition, itemDefinitionMap map[string]ItemDefinition, seen map[string]bool) string {
	if def.IsCollection == "true" {
		return "list<" + resolveInstanceOfType(def.TypeRef, itemDefinitionMap, seen) + ">"
	}
	if len(def.ItemComponent) > 0 {
		parts := make([]string, len(def.ItemComponent))
		for i, comp := range def.ItemComponent {
			parts[i] = comp.Name + ": " + itemDefinitionDescriptor(comp, itemDefinitionMap, seen)
		}
		return "context<" + strings.Join(parts, ", ") + ">"
	}
	if def.TypeRef == "" {
		return "Any"
	}
	return resolveInstanceOfType(def.TypeRef, itemDefinitionMap, seen)
}

// resolveInstanceOfType follows typeRef to either a further itemDefinition
// (recursing, with cycle protection) or a bare primitive name (returned
// as-is).
func resolveInstanceOfType(typeRef string, itemDefinitionMap map[string]ItemDefinition, seen map[string]bool) string {
	if seen[typeRef] {
		return typeRef
	}
	def, ok := itemDefinitionMap[typeRef]
	if !ok {
		return typeRef
	}
	seen[typeRef] = true
	return itemDefinitionDescriptor(def, itemDefinitionMap, seen)
}

// isFEELNull reports whether v is FEEL's null value.
func isFEELNull(v any) bool {
	_, ok := v.(*feel.NullValue)
	return ok
}

// coerceToType coerces value to conform to typeRef (a FEEL primitive name
// or a custom itemDefinition name resolved via itemDefinitionMap), per the
// DMN FEEL type-conformance rules: a value that cannot be made to conform
// becomes null; a singleton list is unwrapped when the target type is not
// itself a collection; a scalar is wrapped into a singleton list when the
// target type is a collection; a context conforms to a structural type when
// it has (at least) every declared component, each itself conforming to its
// declared type (extra components are allowed).
func coerceToType(value any, typeRef string, itemDefinitionMap map[string]ItemDefinition) any {
	if typeRef == "" || isFEELNull(value) {
		return value
	}

	def, hasDef := itemDefinitionMap[typeRef]

	if !(hasDef && def.IsCollection == "true") {
		if list, ok := value.([]any); ok && len(list) == 1 {
			value = list[0]
		}
	}

	if hasDef {
		switch {
		case def.IsCollection == "true":
			return coerceToList(value, def.TypeRef, itemDefinitionMap)
		case len(def.ItemComponent) > 0:
			return coerceToContext(value, def.ItemComponent, itemDefinitionMap)
		case def.TypeRef != "":
			return coerceToType(value, def.TypeRef, itemDefinitionMap)
		default:
			// e.g. a functionItem type: no representable shape to check, accept as-is.
			return value
		}
	}

	return coercePrimitive(value, typeRef)
}

// coerceToList coerces value into a list whose every element conforms to
// elemTypeRef. A non-list value is treated as an implicit singleton list.
func coerceToList(value any, elemTypeRef string, itemDefinitionMap map[string]ItemDefinition) any {
	list, ok := value.([]any)
	if !ok {
		list = []any{value}
	}

	out := make([]any, len(list))
	for i, v := range list {
		coerced := coerceToType(v, elemTypeRef, itemDefinitionMap)
		if isFEELNull(coerced) && !isFEELNull(v) {
			return feel.Null
		}
		out[i] = coerced
	}
	return out
}

// coerceToContext coerces value into a context conforming to a structural
// type: every declared component must be present and itself conform to its
// declared type. Components not declared by the type are left untouched.
func coerceToContext(value any, components []ItemDefinition, itemDefinitionMap map[string]ItemDefinition) any {
	m, ok := value.(map[string]any)
	if !ok {
		return feel.Null
	}

	out := make(map[string]any, len(m))
	maps.Copy(out, m)

	for _, comp := range components {
		v, present := m[comp.Name]
		if !present {
			return feel.Null
		}
		coerced := coerceToType(v, comp.TypeRef, itemDefinitionMap)
		if isFEELNull(coerced) && !isFEELNull(v) {
			return feel.Null
		}
		out[comp.Name] = coerced
	}

	return out
}

// coercePrimitive checks value against a FEEL primitive type name. date,
// time, dateTime, and duration string values (as supplied by callers of
// Evaluate, which have no way to express FEEL-native temporal types) are
// parsed into FEEL's temporal types here so that comparison operators
// (</>/<=/>=) work against them instead of silently returning null on a
// string-vs-temporal type mismatch. Values that are already FEEL-native
// (not a string) are accepted as-is, since this engine doesn't model these
// types precisely enough to validate them beyond that.
func coercePrimitive(value any, typeRef string) any {
	switch typeRef {
	case "number":
		if _, ok := value.(*feel.Number); ok {
			return value
		}
		return feel.Null
	case "string":
		if _, ok := value.(string); ok {
			return value
		}
		return feel.Null
	case "boolean":
		if _, ok := value.(bool); ok {
			return value
		}
		return feel.Null
	case "date":
		if s, ok := value.(string); ok {
			if d, err := feel.ParseDate(s); err == nil {
				return d
			}
			return feel.Null
		}
		return value
	case "time":
		if s, ok := value.(string); ok {
			if t, err := feel.ParseTime(s); err == nil {
				return t
			}
			return feel.Null
		}
		return value
	case "dateTime":
		if s, ok := value.(string); ok {
			if dt, err := feel.ParseDatetime(s); err == nil {
				return dt
			}
			return feel.Null
		}
		return value
	case "duration", "dayTimeDuration", "yearMonthDuration":
		if s, ok := value.(string); ok {
			if d, err := feel.ParseDuration(s); err == nil {
				return d
			}
			return feel.Null
		}
		return value
	default:
		return value
	}
}

// startsWithComparisonOperator reports whether a decision table input entry
// text opens with a range/comparison operator (e.g. ">1", "<=3") rather than
// being a bare value that should be matched by equality.
func startsWithComparisonOperator(text string) bool {
	for _, op := range []string{"<=", ">=", "<", ">"} {
		if strings.HasPrefix(text, op) {
			return true
		}
	}
	return false
}

// evalDecisionTable evaluates a single DMN decisionTable against ctx,
// applying its hit policy (and, for COLLECT, its aggregation) to the rules
// that match. Shared by top-level decision bodies and decisionTables nested
// inside a context entry.
func evalDecisionTable(dt DecisionTable, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition) (any, error) {
	hitPolicy := dt.HitPolicy
	if hitPolicy == "" {
		// Blank/omitted hitPolicy defaults to "Unique" per spec.
		hitPolicy = HitPolicyUnique
	}
	if !IsValidHitPolicy(hitPolicy) {
		return nil, fmt.Errorf("hit policy %s is not valid", hitPolicy)
	}

	var hitsList []any

	for _, rule := range dt.Rules {
		// todo make sure the types are the same from the ctx input to the rule input
		hit := true
		for j, ie := range rule.InputEntries {
			if ie.Text == "-" {
				continue
			}

			input := dt.Inputs[j]

			expression := ie.Text

			switch resolvePrimitiveType(input.InputExpression.TypeRef, itemDefinitionMap) {
			case "number":
				text := strings.TrimSpace(ie.Text)
				if startsWithComparisonOperator(text) {
					expression = fmt.Sprintf("%s %s", input.InputExpression.Text, text)
				} else {
					// A bare number/expression with no comparison operator is
					// an implicit equality test per FEEL unary-tests grammar.
					expression = fmt.Sprintf("%s = %s", input.InputExpression.Text, text)
				}
			default:
				// string, boolean, date, time, dateTime, duration, and
				// structural types: a leading comparison operator is a
				// range/comparison test, otherwise the (possibly
				// comma-separated) cell is a membership-equality test
				// against the input.
				text := strings.TrimSpace(ie.Text)
				if startsWithComparisonOperator(text) {
					expression = fmt.Sprintf("%s %s", input.InputExpression.Text, text)
				} else {
					expression = fmt.Sprintf("list contains([%s], %s)", ie.Text, input.InputExpression.Text)
				}
			}

			ret, err := evalFEEL(expression, ctx, "", nil, itemDefinitionMap)
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
			// A rule with multiple output columns produces one
			// record (keyed by output column name) per match;
			// a single-output rule produces a bare scalar.
			var record any
			if len(dt.Output) > 1 {
				rec := make(map[string]any, len(rule.OutputEntries))
				for oi, oe := range rule.OutputEntries {
					name := ""
					if oi < len(dt.Output) {
						name = dt.Output[oi].Name
					}
					rec[name] = evalOutputEntry(oe.Text, ctx, itemDefinitionMap)
				}
				record = rec
			} else if len(rule.OutputEntries) > 0 {
				record = evalOutputEntry(rule.OutputEntries[0].Text, ctx, itemDefinitionMap)
			}
			hitsList = append(hitsList, record)

			if hitPolicy == HitPolicyFirst {
				break
			}
		}
	}

	var result any = feel.Null

	switch hitPolicy {
	case HitPolicyUnique:
		if len(hitsList) > 1 {
			return nil, fmt.Errorf("decision table had more than one output for unique policy: %+v", hitsList)
		}
		if len(hitsList) == 1 {
			result = hitsList[0]
		}
	case HitPolicyFirst:
		if len(hitsList) > 0 {
			result = hitsList[0]
		}
	case HitPolicyAny:
		if len(hitsList) > 0 {
			result = hitsList[0]
			for _, hit := range hitsList[1:] {
				if !reflect.DeepEqual(hit, result) {
					return nil, fmt.Errorf("decision table had conflicting outputs for ANY policy: %+v", hitsList)
				}
			}
		}
	case HitPolicyPriority:
		if len(hitsList) > 0 {
			best := hitsList[0]
			bestRank, err := priorityRank(dt, best)
			if err != nil {
				return nil, err
			}
			for _, hit := range hitsList[1:] {
				rank, err := priorityRank(dt, hit)
				if err != nil {
					return nil, err
				}
				if lessRank(rank, bestRank) {
					best = hit
					bestRank = rank
				}
			}
			result = best
		}
	case HitPolicyOutputOrder:
		ordered := slices.Clone(hitsList)
		ranks := make([][]int, len(ordered))
		for i, hit := range ordered {
			rank, err := priorityRank(dt, hit)
			if err != nil {
				return nil, err
			}
			ranks[i] = rank
		}
		sort.SliceStable(ordered, func(a, b int) bool {
			return lessRank(ranks[a], ranks[b])
		})

		result = ordered
	case HitPolicyRuleOrder:
		result = hitsList
	case HitPolicyCollect:
		result = aggregateCollect(dt.Aggregation, hitsList)
	}

	return result, nil
}

// priorityRank computes, for each output column of hit, the index of that
// column's value within its outputValues priority list (lower index = higher
// priority). It returns one rank per output column so PRIORITY/OUTPUT ORDER
// can compare hits lexicographically across all output columns, not just the
// first.
func priorityRank(dt DecisionTable, hit any) ([]int, error) {
	rank := make([]int, len(dt.Output))
	for i, out := range dt.Output {
		var val any
		if len(dt.Output) > 1 {
			m, _ := hit.(map[string]any)
			val = m[out.Name]
		} else {
			val = hit
		}

		idx, err := outputValueRank(out, val)
		if err != nil {
			return nil, err
		}
		rank[i] = idx
	}
	return rank, nil
}

// outputValueRank returns the priority index of val within out's
// outputValues list (0 = highest priority). If out has no outputValues
// list, every value ranks equally (0). A value absent from the list ranks
// last, after every declared value.
func outputValueRank(out Output, val any) (int, error) {
	text := strings.TrimSpace(out.OutputValues.Text)
	if text == "" {
		return 0, nil
	}

	parts := strings.Split(text, ",")
	for idx, part := range parts {
		unquoted, err := strconv.Unquote(strings.TrimSpace(part))
		if err != nil {
			return 0, fmt.Errorf("invalid output value %q for output %q: %w", part, out.Name, err)
		}
		if unquoted == val {
			return idx, nil
		}
	}
	return len(parts), nil
}

// lessRank reports whether rank a has strictly higher priority than rank b,
// comparing output columns lexicographically (first column is primary).
func lessRank(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// isFatalEvalError reports whether err represents a genuine evaluation
// crash (e.g. malformed FEEL syntax) as opposed to a FEEL-spec-mandated
// "invalid operation/argument" result, which per the FEEL spec must
// evaluate to null rather than propagate as a hard error (e.g. `1 + true`,
// or calling a built-in with a wrongly typed argument).
func isFatalEvalError(err error) bool {
	var unexpectedToken *feel.UnexpectedToken
	return errors.As(err, &unexpectedToken)
}

func evalContext(c *Context, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any) (any, error) {
	local := make(map[string]any, len(ctx)+len(c.Entries))
	maps.Copy(local, ctx)

	resultMap := map[string]any{}

	var anonResult any
	hasAnon := false

	for _, entry := range c.Entries {
		var val any
		var err error

		switch {
		case entry.Context != nil:
			val, err = evalContext(entry.Context, local, itemDefinitionMap, nativeScope)
		case entry.DecisionTable != nil:
			val, err = evalDecisionTable(*entry.DecisionTable, local, itemDefinitionMap)
		case entry.LiteralExpression != nil:
			val, err = evalFEEL(entry.LiteralExpression.Text, local, "", nativeScope, itemDefinitionMap)
		case entry.Relation != nil:
			val, err = evalRelation(entry.Relation, local, itemDefinitionMap, nativeScope)
		case entry.List != nil:
			val, err = evalList(entry.List, local, itemDefinitionMap, nativeScope)
		case entry.Filter != nil:
			val, err = evalFilter(entry.Filter, local, itemDefinitionMap, nativeScope)
		case entry.For != nil:
			val, err = evalFor(entry.For, local, itemDefinitionMap, nativeScope)
		case entry.Conditional != nil:
			val, err = evalConditional(entry.Conditional, local, itemDefinitionMap, nativeScope)
		case entry.Some != nil:
			val, err = evalQuantified(entry.Some, local, itemDefinitionMap, nativeScope, false)
		case entry.Every != nil:
			val, err = evalQuantified(entry.Every, local, itemDefinitionMap, nativeScope, true)
		default:
			val = feel.Null
		}

		if err != nil {
			if isFatalEvalError(err) {
				return nil, err
			}

			val = feel.Null
		}

		if entry.Variable != nil && entry.Variable.Name != "" {
			local[entry.Variable.Name] = val
			resultMap[entry.Variable.Name] = val
		} else {
			anonResult = val
			hasAnon = true
		}
	}

	if hasAnon {
		return anonResult, nil
	}

	return resultMap, nil
}

// evalExpression evaluates any of the DMN expression variants wrapped in an
// Expression, recursing into nested boxed expressions as needed. It's the
// shared dispatcher used by Relation/List/Filter/For/Conditional/Quantified,
// mirroring the top-level Decision/ContextEntry dispatch.
func evalExpression(e Expression, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any) (any, error) {
	switch {
	case e.LiteralExpression != nil:
		return evalFEEL(e.LiteralExpression.Text, ctx, "", nativeScope, itemDefinitionMap)
	case e.Context != nil:
		return evalContext(e.Context, ctx, itemDefinitionMap, nativeScope)
	case e.FunctionDefinition != nil:
		return evalFEEL(e.FunctionDefinition.FEELFunctionLiteral(), ctx, "", nativeScope, itemDefinitionMap)
	case e.Invocation != nil:
		return evalFEEL(e.Invocation.FEELCallExpression(), ctx, "", nativeScope, itemDefinitionMap)
	case e.DecisionTable != nil:
		return evalDecisionTable(*e.DecisionTable, ctx, itemDefinitionMap)
	case e.Relation != nil:
		return evalRelation(e.Relation, ctx, itemDefinitionMap, nativeScope)
	case e.List != nil:
		return evalList(e.List, ctx, itemDefinitionMap, nativeScope)
	case e.Filter != nil:
		return evalFilter(e.Filter, ctx, itemDefinitionMap, nativeScope)
	case e.For != nil:
		return evalFor(e.For, ctx, itemDefinitionMap, nativeScope)
	case e.Conditional != nil:
		return evalConditional(e.Conditional, ctx, itemDefinitionMap, nativeScope)
	case e.Some != nil:
		return evalQuantified(e.Some, ctx, itemDefinitionMap, nativeScope, false)
	case e.Every != nil:
		return evalQuantified(e.Every, ctx, itemDefinitionMap, nativeScope, true)
	default:
		return feel.Null, nil
	}
}

// evalRelation evaluates a DMN <relation> boxed expression into a list of
// contexts, one per row, keyed by column name.
func evalRelation(r *Relation, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any) (any, error) {
	rows := make([]any, len(r.Rows))

	for i, row := range r.Rows {
		rowMap := make(map[string]any, len(r.Columns))

		for j, col := range r.Columns {
			if j >= len(row.Expressions) {
				rowMap[col.Name] = feel.Null
				continue
			}

			val, err := evalExpression(row.Expressions[j], ctx, itemDefinitionMap, nativeScope)
			if err != nil {
				return nil, err
			}
			rowMap[col.Name] = val
		}

		rows[i] = rowMap
	}

	return rows, nil
}

// evalList evaluates a DMN <list> boxed expression into a plain FEEL list.
func evalList(l *List, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any) (any, error) {
	result := make([]any, len(l.Expressions))

	for i, e := range l.Expressions {
		val, err := evalExpression(e, ctx, itemDefinitionMap, nativeScope)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}

	return result, nil
}

// asIterable normalizes a value being iterated/filtered over: FEEL treats a
// single non-list value the same as a singleton list in these contexts.
func asIterable(v any) []any {
	if items, ok := v.([]any); ok {
		return items
	}
	return []any{v}
}

// evalFilter evaluates a DMN <filter> boxed expression (list[condition]),
// binding the implicit "item" variable to each element in turn.
func evalFilter(f *Filter, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any) (any, error) {
	listVal, err := evalExpression(f.In, ctx, itemDefinitionMap, nativeScope)
	if err != nil {
		return nil, err
	}

	items := asIterable(listVal)
	result := make([]any, 0, len(items))

	for _, item := range items {
		local := make(map[string]any, len(ctx)+1)
		maps.Copy(local, ctx)
		local["item"] = item

		matched, err := evalExpression(f.Match, local, itemDefinitionMap, nativeScope)
		if err != nil {
			return nil, err
		}

		if b, ok := matched.(bool); ok && b {
			result = append(result, item)
		}
	}

	return result, nil
}

// evalFor evaluates a DMN <for> boxed expression: for x in list return expr.
func evalFor(f *For, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any) (any, error) {
	listVal, err := evalExpression(f.In, ctx, itemDefinitionMap, nativeScope)
	if err != nil {
		return nil, err
	}

	items := asIterable(listVal)
	result := make([]any, len(items))

	for i, item := range items {
		local := make(map[string]any, len(ctx)+1)
		maps.Copy(local, ctx)
		local[f.IteratorVariable] = item

		val, err := evalExpression(f.Return, local, itemDefinitionMap, nativeScope)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}

	return result, nil
}

// evalConditional evaluates a DMN <conditional> boxed expression:
// if cond then a else b.
func evalConditional(c *Conditional, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any) (any, error) {
	condVal, err := evalExpression(c.If, ctx, itemDefinitionMap, nativeScope)
	if err != nil {
		return nil, err
	}

	b, ok := condVal.(bool)
	if !ok {
		return nil, fmt.Errorf("expected conditional 'if' to evaluate to a bool, got: %+v", condVal)
	}

	if b {
		return evalExpression(c.Then, ctx, itemDefinitionMap, nativeScope)
	}
	return evalExpression(c.Else, ctx, itemDefinitionMap, nativeScope)
}

// evalQuantified evaluates a DMN <some>/<every> boxed expression, binding the
// iterator variable to each element of <in> in turn and short-circuiting as
// soon as the result is determined.
func evalQuantified(q *Quantified, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition, nativeScope map[string]any, every bool) (any, error) {
	listVal, err := evalExpression(q.In, ctx, itemDefinitionMap, nativeScope)
	if err != nil {
		return nil, err
	}

	for _, item := range asIterable(listVal) {
		local := make(map[string]any, len(ctx)+1)
		maps.Copy(local, ctx)
		local[q.IteratorVariable] = item

		val, err := evalExpression(q.Satisfies, local, itemDefinitionMap, nativeScope)
		if err != nil {
			return nil, err
		}

		b, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("expected 'satisfies' to evaluate to a bool, got: %+v", val)
		}

		if every && !b {
			return false, nil
		}
		if !every && b {
			return true, nil
		}
	}

	return every, nil
}

// evalOutputEntry evaluates a decision table output entry as a FEEL
// expression against ctx, so entries can be literals ("Gold", 98.83) or
// arbitrary formulas referencing the table's inputs (e.g.
// "(Principal*Rate/12)/(1-(1+Rate/12)**-Term)+Fees"). Falls back to the
// unquoted literal text if the expression fails to evaluate.
func evalOutputEntry(text string, ctx map[string]any, itemDefinitionMap map[string]ItemDefinition) any {
	ret, err := evalFEEL(text, ctx, "", nil, itemDefinitionMap)
	if err == nil {
		return ret
	}

	unquoted, err := strconv.Unquote(text)
	if err != nil {
		return text
	}
	return unquoted
}

// aggregateCollect applies a COLLECT hit policy's aggregation function
// (SUM/MIN/MAX/COUNT) to a decision table's collected hits. With no
// aggregation attribute (or a non-numeric aggregation), COLLECT behaves like
// RULE_ORDER and returns the raw list.
func aggregateCollect(aggregation string, hits []any) any {
	if aggregation == "" {
		return hits
	}

	if aggregation == AggregationCount {
		return feel.NewNumberFromInt64(int64(len(hits)))
	}

	if len(hits) == 0 {
		return feel.Null
	}

	nums := make([]*feel.Number, 0, len(hits))
	for _, h := range hits {
		n, err := feel.ParseNumberWithErr(h)
		if err != nil {
			// not a numeric aggregation after all; fall back to the raw list
			return hits
		}
		nums = append(nums, n)
	}

	switch aggregation {
	case AggregationSum:
		result := feel.NewNumberFromInt64(0)
		for _, n := range nums {
			result = result.Add(n)
		}
		return result
	case AggregationMin:
		result := nums[0]
		for _, n := range nums[1:] {
			if n.Cmp(result) < 0 {
				result = n
			}
		}
		return result
	case AggregationMax:
		result := nums[0]
		for _, n := range nums[1:] {
			if n.Cmp(result) > 0 {
				result = n
			}
		}
		return result
	default:
		return hits
	}
}

func explore(nodes map[string]node, edges map[string][]edge, k string, counter *int) map[string]node {
	n := nodes[k]
	n.Visited = true
	n.Pre = *counter
	*counter++
	nodes[k] = n

	for _, e := range edges[k] {
		if !nodes[e.To].Visited {
			explore(nodes, edges, e.To, counter)
		}
	}

	n = nodes[k]
	n.Post = *counter
	*counter++
	nodes[k] = n

	return nodes
}

func dfs(nodes map[string]node, edges map[string][]edge) []node {
	counter := 0

	for k, n := range nodes {
		if n.Visited {
			continue
		}

		explore(nodes, edges, k, &counter)
	}

	nodeList := []node{}

	for _, v := range nodes {
		nodeList = append(nodeList, v)
	}

	return nodeList
}

func (d Definitions) Evaluate(context map[string]any) (map[string]any, error) {
	return d.evaluate(context, nil)
}

// evaluate is Evaluate plus seedDecisions: decision IDs whose value is
// supplied directly rather than computed. It exists for decision-service
// invocation, where an inputDecision parameter substitutes a caller-supplied
// value for a decision that would otherwise need its own upstream inputs.
func (d Definitions) evaluate(context map[string]any, seedDecisions map[string]any) (map[string]any, error) {
	// kept unshadowed so decision-service invocations (below) can recursively
	// re-evaluate the whole document against a fresh set of inputs.
	root := d

	itemDefinitionMap := make(map[string]ItemDefinition, 0)

	d.TopologicalSort()

	for _, v := range d.ItemDefinition {
		itemDefinitionMap[v.Name] = v
	}

	bkmMap := make(map[string]BusinessKnowledgeModel, len(d.BusinessKnowledgeModels))
	for _, v := range d.BusinessKnowledgeModels {
		bkmMap[v.ID] = v
	}

	dsMap := make(map[string]DecisionService, len(d.DecisionServices))
	for _, v := range d.DecisionServices {
		dsMap[v.ID] = v
	}

	inputDataByID := make(map[string]InputData, len(d.InputData))
	for _, v := range d.InputData {
		inputDataByID[v.ID] = v
	}

	// the string key is the variable name of the input
	inputMap := make(map[string]Variable, len(context))

	for _, i := range d.InputData {
		if _, found := context[i.Name]; !found {
			// return nil, fmt.Errorf("%w: %s not found for input: %+v", ErrMissingInput, i.Name, i.ID)
			continue
		}

		// todo make sure the types are the same between input data and the context

		inputMap[i.ID] = i.Variable
	}

	// gets re-used across all Decisions
	ctx := map[string]any{}
	decisionOutputs := map[string]any{}

	for _, d := range d.Decisions {
		if v, seeded := seedDecisions[d.ID]; seeded {
			decisionOutputs[d.ID] = v
			ctx[d.Variable.Name] = v
			continue
		}

		missingInput := false
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
					missingInput = true
					break
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
							return nil, fmt.Errorf("unable to marshal allowed vars: %w", err)
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

				ctx[variable.Name] = coerceToType(toFEELValue(context[variable.Name]), variable.TypeRef, itemDefinitionMap)
			}

			if i.RequiredDecision != nil {
				_, hasDecision := decisionOutputs[i.RequiredDecision.ResolvedID()]
				if !hasDecision {
					return nil, fmt.Errorf("%w: %s not found for decision: %v", ErrMissingInput, i.ID, d.ID)
				}
			}
		}

		if missingInput {
			decisionOutputs[d.ID] = feel.Null
			continue
		}

		var nativeScope map[string]any

		if len(d.KnowledgeRequirements) > 0 {
			nativeScope = map[string]any{}
			for _, kr := range d.KnowledgeRequirements {
				if kr.RequiredKnowledge == nil {
					continue
				}

				resolvedID := kr.RequiredKnowledge.ResolvedID()

				if bkm, hasBKM := bkmMap[resolvedID]; hasBKM {
					nativeScope[bkm.Variable.Name] = bkmFunc(bkm, itemDefinitionMap)
					continue
				}

				ds, hasDS := dsMap[resolvedID]
				if !hasDS {
					return nil, fmt.Errorf("business knowledge model: %s not found for decision: %v", resolvedID, d.ID)
				}

				nativeScope[ds.Variable.Name] = decisionServiceFunc(root, ds, inputDataByID)
			}
		}

		// coerceResult applies the decision's declared output type (if any)
		// to a just-evaluated result, per DMN FEEL type-conformance rules.
		coerceResult := func(v any) any {
			return coerceToType(v, d.Variable.TypeRef, itemDefinitionMap)
		}

		if d.LiteralExpression != nil {
			ret, err := evalFEEL(d.LiteralExpression.Text, ctx, "", nativeScope, itemDefinitionMap)
			if err != nil {
				if isFatalEvalError(err) {
					return nil, fmt.Errorf("unable to eval string: '%s' with ctx: %+v: %w", d.LiteralExpression.Text, ctx, err)
				}

				ret = feel.Null
			}

			ret = coerceResult(ret)
			decisionOutputs[d.ID] = ret
			ctx[d.Variable.Name] = ret
		}

		if d.Context != nil {
			ret, err := evalContext(d.Context, ctx, itemDefinitionMap, nativeScope)
			if err != nil {
				if isFatalEvalError(err) {
					return nil, err
				}

				ret = feel.Null
			}

			ret = coerceResult(ret)
			decisionOutputs[d.ID] = ret
			ctx[d.Variable.Name] = ret
		}

		if d.FunctionDefinition != nil {
			ret, err := evalFEEL(d.FunctionDefinition.FEELFunctionLiteral(), ctx, "", nativeScope, itemDefinitionMap)
			if err != nil {
				if isFatalEvalError(err) {
					return nil, err
				}

				ret = feel.Null
			}

			ret = coerceResult(ret)
			decisionOutputs[d.ID] = ret
			ctx[d.Variable.Name] = ret
		}

		if d.Invocation != nil {
			ret, err := evalFEEL(d.Invocation.FEELCallExpression(), ctx, "", nativeScope, itemDefinitionMap)
			if err != nil {
				if isFatalEvalError(err) {
					return nil, err
				}

				ret = feel.Null
			}

			ret = coerceResult(ret)
			decisionOutputs[d.ID] = ret
			ctx[d.Variable.Name] = ret
		}

		if len(d.DecisionTables) != 0 {
			for _, dt := range d.DecisionTables {
				result, err := evalDecisionTable(dt, ctx, itemDefinitionMap)
				if err != nil {
					return nil, err
				}

				result = coerceResult(result)
				decisionOutputs[d.ID] = result
				ctx[d.Variable.Name] = result
			}
		}

		boxedEvaluators := []struct {
			ok   bool
			eval func() (any, error)
		}{
			{d.Relation != nil, func() (any, error) { return evalRelation(d.Relation, ctx, itemDefinitionMap, nativeScope) }},
			{d.List != nil, func() (any, error) { return evalList(d.List, ctx, itemDefinitionMap, nativeScope) }},
			{d.Filter != nil, func() (any, error) { return evalFilter(d.Filter, ctx, itemDefinitionMap, nativeScope) }},
			{d.For != nil, func() (any, error) { return evalFor(d.For, ctx, itemDefinitionMap, nativeScope) }},
			{d.Conditional != nil, func() (any, error) { return evalConditional(d.Conditional, ctx, itemDefinitionMap, nativeScope) }},
			{d.Some != nil, func() (any, error) { return evalQuantified(d.Some, ctx, itemDefinitionMap, nativeScope, false) }},
			{d.Every != nil, func() (any, error) { return evalQuantified(d.Every, ctx, itemDefinitionMap, nativeScope, true) }},
		}

		for _, be := range boxedEvaluators {
			if !be.ok {
				continue
			}

			ret, err := be.eval()
			if err != nil {
				if isFatalEvalError(err) {
					return nil, err
				}

				ret = feel.Null
			}

			ret = coerceResult(ret)
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

	v, err := versions.DetectVersion(d.XMLName.Space)
	if err != nil {
		fmt.Printf("Unable to detect version: %+v", err)
		return d, err
	}

	d.Version = v

	// The DMN schema requires an id on every decision, but some TCK fixtures
	// omit it on decisions nothing else references; fall back to the name so
	// every decision still gets a distinct key instead of colliding on "".
	for i, dec := range d.Decisions {
		if dec.ID == "" {
			d.Decisions[i].ID = dec.Name
		}
	}

	return d, nil
}
