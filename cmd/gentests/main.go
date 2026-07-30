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
	ID          string          `xml:"id,attr"`
	InputNodes  []tckInputNode  `xml:"inputNode"`
	ResultNodes []tckResultNode `xml:"resultNode"`
}

type tckInputNode struct {
	Name       string         `xml:"name,attr"`
	Value      *tckValue      `xml:"value"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"`
}

type tckComponent struct {
	Name  string   `xml:"name,attr"`
	Value tckValue `xml:"value"`
}

type tckResultNode struct {
	Name     string       `xml:"name,attr"`
	Expected *tckExpected `xml:"expected"`
}

type tckExpected struct {
	Value *tckValue `xml:"value"`
	List  *tckList  `xml:"list"`
}

type tckList struct {
	Items []tckListItem `xml:"item"`
}

type tckListItem struct {
	Value      tckValue       `xml:"value"`
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

// values returns the scalar values carried by a list item: the field values
// of each component for a structured (record) item, or the item's own value
// for a plain scalar item.
func (li tckListItem) values() []tckValue {
	if len(li.Components) > 0 {
		vs := make([]tckValue, len(li.Components))
		for i, c := range li.Components {
			vs[i] = c.Value
		}
		return vs
	}
	return []tckValue{li.Value}
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
	default:
		return strconv.Quote(v.Content)
	}
}

// inputLiteral returns the Go literal for an inputNode.
// Simple values produce a scalar literal; list inputs produce a []any literal;
// component inputs produce a map literal.
func inputLiteral(n tckInputNode) string {
	if n.Value != nil {
		return goLiteral(*n.Value)
	}
	if n.List != nil {
		var sb strings.Builder
		sb.WriteString("[]any{")
		for _, item := range n.List.Items {
			if len(item.Components) > 0 {
				sb.WriteString("map[string]any{")
				for _, c := range item.Components {
					fmt.Fprintf(&sb, "%s: %s,", strconv.Quote(c.Name), goLiteral(c.Value))
				}
				sb.WriteString("},")
			} else {
				fmt.Fprintf(&sb, "%s,", goLiteral(item.Value))
			}
		}
		sb.WriteString("}")
		return sb.String()
	}
	var sb strings.Builder
	sb.WriteString("map[string]any{")
	for _, c := range n.Components {
		fmt.Fprintf(&sb, "%s: %s,", strconv.Quote(c.Name), goLiteral(c.Value))
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
	scalar    string     // non-empty for scalar assertions
	scalarRaw *tckValue  // raw TCK value for scalar (nil for list assertions)
	isList    bool           // true for list assertions
	listItems []tckListItem  // items for list assertions
}

type genTest struct {
	folder  string
	name    string
	dmnPath string
	inputs  []struct{ name, literal string }
	asserts []assertEntry
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

			folder := filepath.Base(path)
			for _, c := range tc.Cases {
				var inputs []struct{ name, literal string }
				for _, in := range c.InputNodes {
					inputs = append(inputs, struct{ name, literal string }{in.Name, inputLiteral(in)})
				}

				var asserts []assertEntry
				for _, rn := range c.ResultNodes {
					if rn.Expected == nil {
						continue
					}
					decID, ok := decisionIDs[rn.Name]
					if !ok {
						fmt.Fprintf(os.Stderr, "warn: no decision for result %q in %s\n", rn.Name, dmnFile)
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
					}
				}

				if len(asserts) == 0 {
					continue
				}

				tests = append(tests, genTest{
					folder:  folder,
					name:    fmt.Sprintf("TestTCK_%s_%s", toIdentifier(folder), toIdentifier(c.ID)),
					dmnPath: dmnFile,
					inputs:  inputs,
					asserts: asserts,
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
	sb.WriteString("\t\"io\"\n")
	sb.WriteString("\t\"os\"\n\n")
	sb.WriteString("\t\"dmn/engine\"\n")
	sb.WriteString(")\n\n")
	sb.WriteString("func mustParse(path string) engine.Definitions {\n")
	sb.WriteString("\tf, err := os.Open(path)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\tdefer f.Close()\n")
	sb.WriteString("\tdata, err := io.ReadAll(f)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\td, err := engine.Parse(data)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\treturn d\n")
	sb.WriteString("}\n")
	return sb.String()
}

func decimalPlaces(s string) int32 {
	if i := strings.Index(s, "."); i >= 0 {
		return int32(len(s) - i - 1)
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
				for _, v := range item.values() {
					if v.isNil() || isNumericXSIType(v.xsiType()) || stringerGoType(v.xsiType()) != "" {
						usesFeel = true
						break outer
					}
				}
			}
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
	sb.WriteString("\t\"github.com/stretchr/testify/require\"\n")
	sb.WriteString(")\n\n")

	for _, fn := range tests {
		fmt.Fprintf(&sb, "func %s(t *testing.T) {\n", fn.name)
		sb.WriteString("\tt.Parallel()\n")
		fmt.Fprintf(&sb, "\td := mustParse(%s)\n", strconv.Quote("../"+fn.dmnPath))
		sb.WriteString("\tinputs := map[string]any{\n")
		for _, in := range fn.inputs {
			fmt.Fprintf(&sb, "\t\t%s: %s,\n", strconv.Quote(in.name), in.literal)
		}
		sb.WriteString("\t}\n")
		sb.WriteString("\tresult, err := d.Evaluate(inputs)\n")
		sb.WriteString("\trequire.NoError(t, err)\n")
		for _, a := range fn.asserts {
			if a.isList {
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
				content := strings.TrimSpace(a.scalarRaw.Content)
				goType := stringerGoType(a.scalarRaw.xsiType())
				fmt.Fprintf(&sb, "\t{\n")
				fmt.Fprintf(&sb, "\t\tactual, ok := result[%s].(%s)\n", strconv.Quote(a.decID), goType)
				fmt.Fprintf(&sb, "\t\trequire.True(t, ok)\n")
				fmt.Fprintf(&sb, "\t\trequire.Equal(t, %s, actual.String())\n", strconv.Quote(content))
				fmt.Fprintf(&sb, "\t}\n")
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
	case "xsd:duration", "xsd:date", "xsd:time", "xsd:dateTime":
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
	fmt.Fprintf(sb, "\t\tvSlice, isSlice := dRes.([]any)\n")
	fmt.Fprintf(sb, "\t\trequire.True(t, isSlice)\n")
	fmt.Fprintf(sb, "\t\trequire.Equal(t, %d, len(vSlice))\n", len(a.listItems))
	for i, item := range a.listItems {
		if len(item.Components) > 0 {
			fmt.Fprintf(sb, "\t\t{\n")
			fmt.Fprintf(sb, "\t\t\tm, ok := vSlice[%d].(map[string]any)\n", i)
			fmt.Fprintf(sb, "\t\t\trequire.True(t, ok)\n")
			for _, c := range item.Components {
				writeScalarValueAssert(sb, fmt.Sprintf("m[%s]", strconv.Quote(c.Name)), c.Value)
			}
			fmt.Fprintf(sb, "\t\t}\n")
			continue
		}
		writeScalarValueAssert(sb, fmt.Sprintf("vSlice[%d]", i), item.Value)
	}
	fmt.Fprintf(sb, "\t}\n")
}
