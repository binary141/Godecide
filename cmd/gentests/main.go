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
		m[dec.Name] = dec.ID
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
	Value tckValue `xml:"value"`
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
			fmt.Fprintf(&sb, "%s,", goLiteral(item.Value))
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
	isList    bool       // true for list assertions
	listItems []tckValue // items for list assertions
}

type genTest struct {
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
						var items []tckValue
						for _, item := range rn.Expected.List.Items {
							items = append(items, item.Value)
						}
						asserts = append(asserts, assertEntry{
							decID:     decID,
							isList:    true,
							listItems: items,
						})
					} else if rn.Expected.Value != nil {
						asserts = append(asserts, assertEntry{
							decID:  decID,
							scalar: goLiteral(*rn.Expected.Value),
						})
					}
				}

				if len(asserts) == 0 {
					continue
				}

				tests = append(tests, genTest{
					name:    fmt.Sprintf("TestTCK_%s_%s", toIdentifier(folder), toIdentifier(c.ID)),
					dmnPath: dmnFile,
					inputs:  inputs,
					asserts: asserts,
				})
			}
		}

		return nil
	})

	src := buildSource(tests)

	formatted, err := format.Source([]byte(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "format error: %v\n", err)
		formatted = []byte(src)
	}

	if err := os.WriteFile("generated_tck_test.go", formatted, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote generated_tck_test.go (%d tests)\n", len(tests))
}

func buildSource(tests []genTest) string {
	var sb strings.Builder

	sb.WriteString("// Code generated by cmd/gentests/main.go; DO NOT EDIT.\n")
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"io\"\n")
	sb.WriteString("\t\"os\"\n")
	sb.WriteString("\t\"testing\"\n\n")
	sb.WriteString("\tfeel \"github.com/binary141/FEEL.go\"\n")
	sb.WriteString("\t\"github.com/stretchr/testify/require\"\n")
	sb.WriteString(")\n\n")

	sb.WriteString("func mustParse(path string) Definitions {\n")
	sb.WriteString("\tf, err := os.Open(path)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\tdefer f.Close()\n")
	sb.WriteString("\tdata, err := io.ReadAll(f)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\td, err := Parse(data)\n")
	sb.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	sb.WriteString("\treturn d\n")
	sb.WriteString("}\n")

	for _, fn := range tests {
		fmt.Fprintf(&sb, "\nfunc %s(t *testing.T) {\n", fn.name)
		fmt.Fprintf(&sb, "\td := mustParse(%s)\n", strconv.Quote(fn.dmnPath))
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
			} else {
				fmt.Fprintf(&sb, "\trequire.Equal(t, %s, result[%s])\n", a.scalar, strconv.Quote(a.decID))
			}
		}
		sb.WriteString("}\n")
	}

	return sb.String()
}

func writeListAssert(sb *strings.Builder, a assertEntry) {
	fmt.Fprintf(sb, "\t{\n")
	fmt.Fprintf(sb, "\t\tdRes := result[%s]\n", strconv.Quote(a.decID))
	fmt.Fprintf(sb, "\t\tvSlice, isSlice := dRes.([]any)\n")
	fmt.Fprintf(sb, "\t\trequire.True(t, isSlice)\n")
	fmt.Fprintf(sb, "\t\trequire.Equal(t, %d, len(vSlice))\n", len(a.listItems))
	for i, item := range a.listItems {
		if item.isNil() {
			fmt.Fprintf(sb, "\t\trequire.Equal(t, feel.Null, vSlice[%d])\n", i)
		} else {
			switch item.xsiType() {
			case "xsd:decimal", "xsd:double", "xsd:float", "xsd:integer", "xsd:long", "xsd:int":
				fVal, err := strconv.ParseFloat(strings.TrimSpace(item.Content), 64)
				if err == nil {
					fmt.Fprintf(sb, "\t\t{\n")
					fmt.Fprintf(sb, "\t\t\tactual, ok := vSlice[%d].(*feel.Number)\n", i)
					fmt.Fprintf(sb, "\t\t\trequire.True(t, ok)\n")
					fmt.Fprintf(sb, "\t\t\trequire.Equal(t, int64(%d), actual.Int64())\n", int64(fVal))
					fmt.Fprintf(sb, "\t\t}\n")
				}
			case "xsd:string":
				fmt.Fprintf(sb, "\t\trequire.Equal(t, %s, vSlice[%d])\n", strconv.Quote(item.Content), i)
			case "xsd:boolean":
				fmt.Fprintf(sb, "\t\trequire.Equal(t, %s, vSlice[%d])\n", strings.TrimSpace(item.Content), i)
			default:
				fmt.Fprintf(sb, "\t\trequire.Equal(t, %s, vSlice[%d])\n", strconv.Quote(item.Content), i)
			}
		}
	}
	fmt.Fprintf(sb, "\t}\n")
}
