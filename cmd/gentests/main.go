// gentests walks testdata/tck/TestCases, finds every DMN+test-XML pair, and
// writes generated_tck_test.go in the module root.
//
// Run via: go generate (see //go:generate in main.go)
package main

import (
	"encoding/xml"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ----- minimal DMN structs (only what we need for decision name→ID lookup) -----

type dmnDefs struct {
	Decisions []dmnDecision `xml:"decision"`
}

type dmnDecision struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

func loadDecisionIDs(dmnPath string) (map[string]string, error) {
	data, err := os.ReadFile(dmnPath)
	if err != nil {
		return nil, err
	}
	var d dmnDefs
	if err := xml.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	m := make(map[string]string, len(d.Decisions))
	for _, dec := range d.Decisions {
		id := dec.ID
		if id == "" {
			id = dec.Name
		}
		m[dec.Name] = id
	}
	return m, nil
}

// ----- TCK test-XML structs -----

type tckTestCases struct {
	Cases []tckTestCase `xml:"testCase"`
}

type tckTestCase struct {
	ID            string          `xml:"id,attr"`
	Type          string          `xml:"type,attr"`
	InvocableName string          `xml:"invocableName,attr"`
	InputNodes    []tckInputNode  `xml:"inputNode"`
	ResultNodes   []tckResultNode `xml:"resultNode"`
}

type tckInputNode struct {
	Name       string         `xml:"name,attr"`
	Value      *tckValue      `xml:"value"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"`
}

type tckComponent struct {
	Name       string         `xml:"name,attr"`
	Value      tckValue       `xml:"value"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"` // nested context: a component whose own value is a structure
}

// componentUsesFeel reports whether a component (including any it nests)
// carries a value that requires the feel package to assert against.
func componentUsesFeel(c tckComponent) bool {
	if len(c.Components) > 0 {
		for _, cc := range c.Components {
			if componentUsesFeel(cc) {
				return true
			}
		}
		return false
	}
	if c.List != nil {
		for _, it := range c.List.Items {
			if itemUsesFeel(it) {
				return true
			}
		}
		return false
	}
	return c.Value.isNil() || isNumericXSIType(c.Value.xsiType()) || stringerGoType(c.Value.xsiType()) != ""
}

type tckResultNode struct {
	Name     string       `xml:"name,attr"`
	Expected *tckExpected `xml:"expected"`
}

type tckExpected struct {
	Value      *tckValue      `xml:"value"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"`
}

type tckList struct {
	Items []tckListItem `xml:"item"`
}

type tckListItem struct {
	Value      tckValue       `xml:"value"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"`
}

type tckValue struct {
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",chardata"`
}

func (v tckValue) xsiType() string {
	for _, a := range v.Attrs {
		if a.Name.Local == "type" {
			return a.Value
		}
	}
	return ""
}

func (v tckValue) isNil() bool {
	for _, a := range v.Attrs {
		if a.Name.Local == "nil" && a.Value == "true" {
			return true
		}
	}
	return false
}

func parseTestXML(path string) (tckTestCases, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tckTestCases{}, err
	}
	var tc tckTestCases
	return tc, xml.Unmarshal(data, &tc)
}

// ----- code-generation helpers -----

func goLiteral(v tckValue) string {
	if v.isNil() {
		return "feel.Null"
	}
	switch v.xsiType() {
	case "xsd:string":
		return strconv.Quote(v.Content)
	case "xsd:decimal", "xsd:double", "xsd:float":
		return "float64(" + v.Content + ")"
	case "xsd:integer", "xsd:long", "xsd:int":
		return "int64(" + v.Content + ")"
	case "xsd:boolean":
		return v.Content // "true" or "false"
	case "xsd:date", "xsd:time", "xsd:dateTime", "xsd:duration":
		// Parse through the FEEL "@" temporal literal at test run time
		// rather than passing the raw ISO text, so date/time/duration
		// -typed inputs reach the engine as real FEEL values (matching how
		// the TCK's xsi:type declares them) instead of bare Go strings.
		return "mustFeelValue(" + strconv.Quote("@"+strconv.Quote(v.Content)) + ")"
	default:
		return strconv.Quote(v.Content)
	}
}

// componentLiteral returns the Go literal for a single component: a nested
// map literal if it itself has sub-components (a struct nested inside a
// struct, e.g. "Applicant data.Monthly" being {Income, Repayments,
// Expenses}), a list literal if it has a list value, or a scalar literal
// otherwise. Components can nest arbitrarily deep, so this recurses rather
// than assuming a component's own <value> child is always present.
func componentLiteral(c tckComponent) string {
	if len(c.Components) > 0 {
		var sb strings.Builder
		sb.WriteString("map[string]any{")
		for _, cc := range c.Components {
			fmt.Fprintf(&sb, "%s: %s,", strconv.Quote(cc.Name), componentLiteral(cc))
		}
		sb.WriteString("}")
		return sb.String()
	}
	if c.List != nil {
		return listLiteral(*c.List)
	}
	return goLiteral(c.Value)
}

// itemLiteral returns the Go literal for a single list item: a nested list
// literal (recursively), a map literal for a structured (component) item, or
// a scalar literal otherwise.
func itemLiteral(item tckListItem) string {
	if item.List != nil {
		return listLiteral(*item.List)
	}
	if len(item.Components) > 0 {
		var sb strings.Builder
		sb.WriteString("map[string]any{")
		for _, c := range item.Components {
			fmt.Fprintf(&sb, "%s: %s,", strconv.Quote(c.Name), componentLiteral(c))
		}
		sb.WriteString("}")
		return sb.String()
	}
	return goLiteral(item.Value)
}

func listLiteral(l tckList) string {
	var sb strings.Builder
	sb.WriteString("[]any{")
	for _, item := range l.Items {
		fmt.Fprintf(&sb, "%s,", itemLiteral(item))
	}
	sb.WriteString("}")
	return sb.String()
}

// inputLiteral returns the Go literal for an inputNode.
// Simple values produce a scalar literal; list inputs produce a []any literal
// (with nested lists-of-lists handled recursively); component inputs produce
// a map literal.
func inputLiteral(n tckInputNode) string {
	if n.Value != nil {
		return goLiteral(*n.Value)
	}
	if n.List != nil {
		return listLiteral(*n.List)
	}
	var sb strings.Builder
	sb.WriteString("map[string]any{")
	for _, c := range n.Components {
		fmt.Fprintf(&sb, "%s: %s,", strconv.Quote(c.Name), componentLiteral(c))
	}
	sb.WriteString("}")
	return sb.String()
}

func toIdentifier(s string) string {
	return strings.NewReplacer("-", "_", " ", "_", ".", "_").Replace(s)
}

// ----- main -----

type assertEntry struct {
	decID     string
	scalar    string        // non-empty for scalar assertions
	scalarRaw *tckValue     // raw TCK value for scalar (nil for list assertions)
	isList    bool          // true for list assertions
	listItems []tckListItem // items for list assertions

	isStruct         bool           // true for structural (context/component) assertions
	structComponents []tckComponent // components for structural assertions
}

type genTest struct {
	folder        string
	name          string
	dmnPath       string
	invocableName string // non-empty: invoke this decisionService instead of evaluating the whole model
	inputs        []struct{ name, literal string }
	asserts       []assertEntry
	skip          string // non-empty: emit a t.Skip with this reason instead of a real test
}

func main() {
	tckRoot := filepath.Join("testdata", "tck", "TestCases")

	var tests []genTest

	_ = filepath.WalkDir(tckRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == tckRoot {
			return err
		}

		// Only generate tests from compliance-level-2 and compliance-level-3.
		rel, _ := filepath.Rel(tckRoot, path)
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if topLevel != "compliance-level-2" && topLevel != "compliance-level-3" {
			return fs.SkipDir
		}

		// Java external function invocation tests are not supported; they are
		// still emitted, as skipped tests, so they show up in the totals.
		javaExternal := filepath.Base(path) == "0076-feel-external-java"

		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}

		var dmnFile string
		var xmlFiles []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			switch {
			case strings.HasSuffix(name, ".dmn"):
				dmnFile = filepath.Join(path, name)
			case strings.HasSuffix(name, ".xml") && strings.Contains(name, "-test-"):
				xmlFiles = append(xmlFiles, filepath.Join(path, name))
			}
		}

		if dmnFile == "" || len(xmlFiles) == 0 {
			return nil
		}

		decisionIDs, err := loadDecisionIDs(dmnFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: parse dmn %s: %v\n", dmnFile, err)
			return nil
		}

		sort.Strings(xmlFiles)
		for _, xmlFile := range xmlFiles {
			tc, err := parseTestXML(xmlFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: parse xml %s: %v\n", xmlFile, err)
				continue
			}

			for _, c := range tc.Cases {
				var inputs []struct{ name, literal string }
				for _, in := range c.InputNodes {
					inputs = append(inputs, struct{ name, literal string }{in.Name, inputLiteral(in)})
				}

				folder := filepath.Base(path)
				if javaExternal {
					tests = append(tests, genTest{
						folder: folder,
						name:   fmt.Sprintf("TestTCK_%s_%s", toIdentifier(folder), toIdentifier(c.ID)),
						skip:   "Java external functions are not supported",
					})
					continue
				}

				unresolved := false
				var asserts []assertEntry
				for _, rn := range c.ResultNodes {
					if rn.Expected == nil {
						continue
					}
					decID, ok := decisionIDs[rn.Name]
					if !ok {
						fmt.Fprintf(os.Stderr, "warn: no decision for result %q in %s\n", rn.Name, dmnFile)
						unresolved = true
						continue
					}
					if rn.Expected.List != nil {
						items := rn.Expected.List.Items
						asserts = append(asserts, assertEntry{
							decID:     decID,
							isList:    true,
							listItems: items,
						})
					} else if rn.Expected.Value != nil {
						asserts = append(asserts, assertEntry{
							decID:     decID,
							scalar:    goLiteral(*rn.Expected.Value),
							scalarRaw: rn.Expected.Value,
						})
					} else if len(rn.Expected.Components) > 0 {
						asserts = append(asserts, assertEntry{
							decID:            decID,
							isStruct:         true,
							structComponents: rn.Expected.Components,
						})
					}
				}

				if len(asserts) == 0 {
					if unresolved {
						tests = append(tests, genTest{
							folder: folder,
							name:   fmt.Sprintf("TestTCK_%s_%s", toIdentifier(folder), toIdentifier(c.ID)),
							skip:   "result decision not found (model imports are not supported)",
						})
					}
					continue
				}

				var invocableName string
				if c.Type == "decisionService" {
					invocableName = c.InvocableName
				}

				tests = append(tests, genTest{
					folder:        folder,
					name:          fmt.Sprintf("TestTCK_%s_%s", toIdentifier(folder), toIdentifier(c.ID)),
					dmnPath:       dmnFile,
					invocableName: invocableName,
					inputs:        inputs,
					asserts:       asserts,
				})
			}
		}

		return nil
	})

	const outDir = "tests"

	// Remove stale generated files from previous runs.
	existing, _ := filepath.Glob(filepath.Join(outDir, "generated_tck_*.go"))
	for _, f := range existing {
		os.Remove(f)
	}

	// Group tests by folder.
	byFolder := make(map[string][]genTest)
	var folderOrder []string
	for _, t := range tests {
		if _, seen := byFolder[t.folder]; !seen {
			folderOrder = append(folderOrder, t.folder)
		}
		byFolder[t.folder] = append(byFolder[t.folder], t)
	}
	sort.Strings(folderOrder)

	// Write the shared helper file.
	helperSrc := buildHelperSource()
	writeFormatted(filepath.Join(outDir, "generated_tck_helpers_test.go"), helperSrc)

	// Write one file per folder.
	total := 0
	for _, folder := range folderOrder {
		group := byFolder[folder]
		src := buildFolderSource(group)
		fname := filepath.Join(outDir, fmt.Sprintf("generated_tck_%s_test.go", toIdentifier(folder)))
		writeFormatted(fname, src)
		total += len(group)
	}

	fmt.Printf("wrote %d files, %d tests\n", len(folderOrder)+1, total)
}

func writeFormatted(path, src string) {
	formatted, err := format.Source([]byte(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "format error in %s: %v\n", path, err)
		formatted = []byte(src)
	}
	if err := os.WriteFile(path, formatted, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", path, err)
		os.Exit(1)
	}
}

func buildHelperSource() string {
	var sb strings.Builder
	sb.WriteString("// Code generated by cmd/gentests/main.go; DO NOT EDIT.\n")
	sb.WriteString("package tests\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"os\"\n")
	sb.WriteString("\t\"sync\"\n\n")
	sb.WriteString("\t\"dmn/engine\"\n")
	sb.WriteString("\tfeel \"github.com/binary141/FEEL.go\"\n")
	sb.WriteString(")\n\n")
	sb.WriteString("// parseCache memoizes mustParse by path: many generated tests share the\n")
	sb.WriteString("// same (sometimes large) DMN file, and re-parsing it from scratch for every\n")
	sb.WriteString("// single test made the suite far slower than it needed to be. A parsed\n")
	sb.WriteString("// engine.Definitions is read-only from Evaluate's perspective, so sharing\n")
	sb.WriteString("// one across parallel tests is safe.\n")
	sb.WriteString("var parseCache sync.Map // path string -> engine.Definitions\n\n")
	sb.WriteString("func mustParse(path string) engine.Definitions {\n")
	sb.WriteString("\tif d, ok := parseCache.Load(path); ok {\n")
	sb.WriteString("\t\treturn d.(engine.Definitions)\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\tdata, err := os.ReadFile(path)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\td, err := engine.Parse(data)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\tactual, _ := parseCache.LoadOrStore(path, d)\n")
	sb.WriteString("\treturn actual.(engine.Definitions)\n")
	sb.WriteString("}\n\n")
	sb.WriteString("// mustFeelValue parses a FEEL literal (e.g. an `@\"...\"` temporal\n")
	sb.WriteString("// literal) at test run time, for TCK inputs whose xsi:type declares a\n")
	sb.WriteString("// date/time/dateTime/duration value rather than a plain string.\n")
	sb.WriteString("func mustFeelValue(expr string) any {\n")
	sb.WriteString("\tv, err := feel.EvalString(expr)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\treturn v\n")
	sb.WriteString("}\n")
	return sb.String()
}

// maxCompareDecimalPlaces caps how many decimal places a generated numeric
// assertion compares. The TCK's expected values for long fractions were
// produced with limited (roughly double) precision and are off in the last
// digits from the exact decimal128 result, so comparing every published digit
// reports false failures.
const maxCompareDecimalPlaces = 10

func decimalPlaces(s string) int32 {
	if i := strings.Index(s, "."); i >= 0 {
		return min(int32(len(s)-i-1), maxCompareDecimalPlaces)
	}
	return 0
}

func isNumericXSIType(t string) bool {
	switch t {
	case "xsd:decimal", "xsd:double", "xsd:float", "xsd:integer", "xsd:long", "xsd:int":
		return true
	}
	return false
}

// stringerGoType returns the Go FEEL type that xsi type t evaluates to when
// it isn't a plain scalar (duration, date, time, dateTime all wrap a
// time.Time and compare by their String() form rather than by equality with
// a Go string), or "" if t is a plain scalar.
func stringerGoType(t string) string {
	switch t {
	case "xsd:duration":
		return "*feel.FEELDuration"
	case "xsd:date":
		return "*feel.FEELDate"
	case "xsd:time":
		return "*feel.FEELTime"
	case "xsd:dateTime":
		return "*feel.FEELDatetime"
	}
	return ""
}

// itemUsesFeel reports whether a list item (possibly a nested list or a
// structured/component item) contains a value that requires the feel
// package to assert against.
func itemUsesFeel(item tckListItem) bool {
	if item.List != nil {
		for _, it := range item.List.Items {
			if itemUsesFeel(it) {
				return true
			}
		}
		return false
	}
	if len(item.Components) > 0 {
		for _, c := range item.Components {
			if componentUsesFeel(c) {
				return true
			}
		}
		return false
	}
	v := item.Value
	return v.isNil() || isNumericXSIType(v.xsiType()) || stringerGoType(v.xsiType()) != ""
}

// uniqueDecIDs returns the distinct decision IDs asserts references, in
// first-seen order.
func uniqueDecIDs(asserts []assertEntry) []string {
	seen := map[string]bool{}
	var ids []string
	for _, a := range asserts {
		if !seen[a.decID] {
			seen[a.decID] = true
			ids = append(ids, a.decID)
		}
	}
	return ids
}

func buildFolderSource(tests []genTest) string {
	// Determine whether any test in this folder references the feel package
	// (feel.Null for nil values, feel.Number for numeric assertions).
	usesFeel := false
outer:
	for _, fn := range tests {
		for _, in := range fn.inputs {
			if in.literal == "feel.Null" || strings.Contains(in.literal, "feel.Null") {
				usesFeel = true
				break outer
			}
		}
		for _, a := range fn.asserts {
			if a.isStruct {
				for _, c := range a.structComponents {
					if componentUsesFeel(c) {
						usesFeel = true
						break outer
					}
				}
				continue
			}
			if !a.isList {
				if a.scalar == "feel.Null" {
					usesFeel = true
					break outer
				}
				if a.scalarRaw != nil && (isNumericXSIType(a.scalarRaw.xsiType()) || stringerGoType(a.scalarRaw.xsiType()) != "") {
					usesFeel = true
					break outer
				}
				continue
			}
			for _, item := range a.listItems {
				if itemUsesFeel(item) {
					usesFeel = true
					break outer
				}
			}
		}
	}

	allSkipped := true
	for _, fn := range tests {
		if fn.skip == "" {
			allSkipped = false
			break
		}
	}

	var sb strings.Builder
	sb.WriteString("// Code generated by cmd/gentests/main.go; DO NOT EDIT.\n")
	sb.WriteString("package tests\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"testing\"\n\n")
	if usesFeel {
		sb.WriteString("\tfeel \"github.com/binary141/FEEL.go\"\n")
	}
	if !allSkipped {
		sb.WriteString("\t\"github.com/stretchr/testify/require\"\n")
	}
	sb.WriteString(")\n\n")

	for _, fn := range tests {
		if fn.skip != "" {
			fmt.Fprintf(&sb, "func %s(t *testing.T) {\n\tt.Skip(%s)\n}\n\n", fn.name, strconv.Quote(fn.skip))
			continue
		}
		fmt.Fprintf(&sb, "func %s(t *testing.T) {\n", fn.name)
		sb.WriteString("\tt.Parallel()\n")
		fmt.Fprintf(&sb, "\td := mustParse(%s)\n", strconv.Quote("../"+fn.dmnPath))
		sb.WriteString("\tinputs := map[string]any{\n")
		for _, in := range fn.inputs {
			fmt.Fprintf(&sb, "\t\t%s: %s,\n", strconv.Quote(in.name), in.literal)
		}
		sb.WriteString("\t}\n")
		if fn.invocableName != "" {
			fmt.Fprintf(&sb, "\tresult, err := d.EvaluateService(%s, inputs)\n", strconv.Quote(fn.invocableName))
		} else {
			// Only compute the decision(s) this test actually asserts on
			// (plus their transitive dependencies) instead of every
			// decision in the model - some TCK files pack hundreds of
			// unrelated decisions into one document, and evaluating all of
			// them for every single test made the suite very slow.
			wantIDs := uniqueDecIDs(fn.asserts)
			quoted := make([]string, len(wantIDs))
			for i, id := range wantIDs {
				quoted[i] = strconv.Quote(id)
			}
			fmt.Fprintf(&sb, "\tresult, err := d.EvaluateDecisions(inputs, %s)\n", strings.Join(quoted, ", "))
		}
		sb.WriteString("\trequire.NoError(t, err)\n")
		for _, a := range fn.asserts {
			if a.isStruct {
				fmt.Fprintf(&sb, "\t{\n")
				fmt.Fprintf(&sb, "\t\tm, ok := result[%s].(map[string]any)\n", strconv.Quote(a.decID))
				fmt.Fprintf(&sb, "\t\trequire.True(t, ok)\n")
				for _, c := range a.structComponents {
					writeComponentAssert(&sb, fmt.Sprintf("m[%s]", strconv.Quote(c.Name)), c)
				}
				fmt.Fprintf(&sb, "\t}\n")
			} else if a.isList {
				writeListAssert(&sb, a)
			} else if a.scalarRaw != nil && isNumericXSIType(a.scalarRaw.xsiType()) {
				content := strings.TrimSpace(a.scalarRaw.Content)
				dp := decimalPlaces(content)
				fmt.Fprintf(&sb, "\t{\n")
				fmt.Fprintf(&sb, "\t\tactual, ok := result[%s].(*feel.Number)\n", strconv.Quote(a.decID))
				fmt.Fprintf(&sb, "\t\trequire.True(t, ok)\n")
				fmt.Fprintf(&sb, "\t\trequire.Equal(t, 0, actual.CompareRounded(*feel.NewNumber(%s), %d))\n", strconv.Quote(content), dp)
				fmt.Fprintf(&sb, "\t}\n")
			} else if a.scalarRaw != nil && stringerGoType(a.scalarRaw.xsiType()) != "" {
				actualExpr := fmt.Sprintf("result[%s]", strconv.Quote(a.decID))
				writeScalarValueAssert(&sb, actualExpr, *a.scalarRaw)
			} else {
				fmt.Fprintf(&sb, "\trequire.Equal(t, %s, result[%s])\n", a.scalar, strconv.Quote(a.decID))
			}
		}
		sb.WriteString("}\n\n")
	}

	return sb.String()
}

// writeScalarValueAssert emits an assertion comparing the Go expression
// actualExpr against the given TCK-expected scalar value.
func writeScalarValueAssert(sb *strings.Builder, actualExpr string, v tckValue) {
	if v.isNil() {
		fmt.Fprintf(sb, "\t\trequire.Equal(t, feel.Null, %s)\n", actualExpr)
		return
	}
	switch v.xsiType() {
	case "xsd:decimal", "xsd:double", "xsd:float", "xsd:integer", "xsd:long", "xsd:int":
		fVal, err := strconv.ParseFloat(strings.TrimSpace(v.Content), 64)
		if err == nil {
			fmt.Fprintf(sb, "\t\t{\n")
			fmt.Fprintf(sb, "\t\t\tactual, ok := (%s).(*feel.Number)\n", actualExpr)
			fmt.Fprintf(sb, "\t\t\trequire.True(t, ok)\n")
			fmt.Fprintf(sb, "\t\t\trequire.Equal(t, int64(%d), actual.Int64())\n", int64(fVal))
			fmt.Fprintf(sb, "\t\t}\n")
		}
	case "xsd:duration":
		// Durations have more than one valid textual form for the same value
		// (e.g. "P0Y" and "P0M" both denote a zero year-month duration), so
		// compare normalized values rather than the exact ISO-8601 text -
		// this mirrors how the official TCK grader compares durations.
		fmt.Fprintf(sb, "\t\t{\n")
		fmt.Fprintf(sb, "\t\t\tactual, ok := (%s).(*feel.FEELDuration)\n", actualExpr)
		fmt.Fprintf(sb, "\t\t\trequire.True(t, ok)\n")
		fmt.Fprintf(sb, "\t\t\texpected, err := feel.ParseDuration(%s)\n", strconv.Quote(strings.TrimSpace(v.Content)))
		fmt.Fprintf(sb, "\t\t\trequire.NoError(t, err)\n")
		fmt.Fprintf(sb, "\t\t\tif expected.IsYearMonth() {\n")
		fmt.Fprintf(sb, "\t\t\t\trequire.True(t, actual.IsYearMonth())\n")
		fmt.Fprintf(sb, "\t\t\t\trequire.Equal(t, expected.TotalMonths(), actual.TotalMonths())\n")
		fmt.Fprintf(sb, "\t\t\t} else {\n")
		fmt.Fprintf(sb, "\t\t\t\trequire.False(t, actual.IsYearMonth())\n")
		fmt.Fprintf(sb, "\t\t\t\trequire.Equal(t, expected.Duration(), actual.Duration())\n")
		fmt.Fprintf(sb, "\t\t\t}\n")
		fmt.Fprintf(sb, "\t\t}\n")
	case "xsd:date", "xsd:time", "xsd:dateTime":
		fmt.Fprintf(sb, "\t\t{\n")
		fmt.Fprintf(sb, "\t\t\tactual, ok := (%s).(%s)\n", actualExpr, stringerGoType(v.xsiType()))
		fmt.Fprintf(sb, "\t\t\trequire.True(t, ok)\n")
		fmt.Fprintf(sb, "\t\t\trequire.Equal(t, %s, actual.String())\n", strconv.Quote(strings.TrimSpace(v.Content)))
		fmt.Fprintf(sb, "\t\t}\n")
	case "xsd:string":
		fmt.Fprintf(sb, "\t\trequire.Equal(t, %s, %s)\n", strconv.Quote(v.Content), actualExpr)
	case "xsd:boolean":
		fmt.Fprintf(sb, "\t\trequire.Equal(t, %s, %s)\n", strings.TrimSpace(v.Content), actualExpr)
	default:
		fmt.Fprintf(sb, "\t\trequire.Equal(t, %s, %s)\n", strconv.Quote(v.Content), actualExpr)
	}
}

func writeListAssert(sb *strings.Builder, a assertEntry) {
	fmt.Fprintf(sb, "\t{\n")
	fmt.Fprintf(sb, "\t\tdRes := result[%s]\n", strconv.Quote(a.decID))
	writeListValueAssert(sb, "dRes", tckList{Items: a.listItems})
	fmt.Fprintf(sb, "\t}\n")
}

// writeListValueAssert emits an assertion that the Go expression actualExpr
// is a []any matching the given expected list, recursing into nested lists
// and structured (component) items.
func writeListValueAssert(sb *strings.Builder, actualExpr string, list tckList) {
	fmt.Fprintf(sb, "\t{\n")
	fmt.Fprintf(sb, "\t\tvSlice, isSlice := (%s).([]any)\n", actualExpr)
	fmt.Fprintf(sb, "\t\trequire.True(t, isSlice)\n")
	fmt.Fprintf(sb, "\t\trequire.Equal(t, %d, len(vSlice))\n", len(list.Items))
	for i, item := range list.Items {
		writeItemAssert(sb, fmt.Sprintf("vSlice[%d]", i), item)
	}
	fmt.Fprintf(sb, "\t}\n")
}

// writeItemAssert emits an assertion for a single expected list item, which
// may itself be a nested list, a structured (component) item, or a scalar.
func writeItemAssert(sb *strings.Builder, actualExpr string, item tckListItem) {
	if item.List != nil {
		writeListValueAssert(sb, actualExpr, *item.List)
		return
	}
	if len(item.Components) > 0 {
		fmt.Fprintf(sb, "\t{\n")
		fmt.Fprintf(sb, "\t\tm, ok := (%s).(map[string]any)\n", actualExpr)
		fmt.Fprintf(sb, "\t\trequire.True(t, ok)\n")
		for _, c := range item.Components {
			writeComponentAssert(sb, fmt.Sprintf("m[%s]", strconv.Quote(c.Name)), c)
		}
		fmt.Fprintf(sb, "\t}\n")
		return
	}
	writeScalarValueAssert(sb, actualExpr, item.Value)
}

// writeComponentAssert emits an assertion for a single expected structural
// component, which may itself nest further components (a sub-context), a
// list, or be a plain scalar value.
func writeComponentAssert(sb *strings.Builder, actualExpr string, c tckComponent) {
	if len(c.Components) > 0 {
		fmt.Fprintf(sb, "\t\t{\n")
		fmt.Fprintf(sb, "\t\t\tcm, ok := (%s).(map[string]any)\n", actualExpr)
		fmt.Fprintf(sb, "\t\t\trequire.True(t, ok)\n")
		for _, cc := range c.Components {
			writeComponentAssert(sb, fmt.Sprintf("cm[%s]", strconv.Quote(cc.Name)), cc)
		}
		fmt.Fprintf(sb, "\t\t}\n")
		return
	}
	if c.List != nil {
		writeListValueAssert(sb, actualExpr, *c.List)
		return
	}
	writeScalarValueAssert(sb, actualExpr, c.Value)
}
