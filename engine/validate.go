package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidateDefinitions performs static, deploy-time checks that go beyond
// what XML parsing enforces. Today it flags decision tables with a UNIQUE
// hit policy whose rules can provably both match the same input - the DMN
// spec treats that as a runtime error (more than one rule matching), but
// for numeric and discrete-value columns it's detectable up front, before
// the table is ever evaluated.
//
// This only reasons about numeric ranges/comparisons and discrete literal
// sets (the common cases). Entries it can't confidently analyze - FEEL
// function calls, "not(...)" tests, arbitrary expressions - are treated as
// non-overlapping for that column rather than flagged, so unusual but
// valid tables aren't rejected on a false positive.
func ValidateDefinitions(d Definitions) []string {
	var problems []string
	for _, dec := range d.Decisions {
		for ti, dt := range dec.DecisionTables {
			problems = append(problems, validateDecisionTable(dec.Name, ti, dt)...)
		}
	}
	problems = append(problems, validateBKMCycles(d)...)
	return problems
}

// validateBKMCycles flags any businessKnowledgeModel whose knowledgeRequirement
// graph, directly or transitively, requires itself. A BKM's requirements
// should form a DAG; evaluating a cyclical one recurses without bound while
// building its native call scope (see bkmFuncSeen), so this is caught here
// instead of at evaluation time.
func validateBKMCycles(d Definitions) []string {
	bkmByID := make(map[string]BusinessKnowledgeModel, len(d.BusinessKnowledgeModels))
	for _, bkm := range d.BusinessKnowledgeModels {
		bkmByID[bkm.ID] = bkm
	}

	var problems []string
	reported := map[string]bool{}

	var visit func(id string, path []string, onPath map[string]bool)
	visit = func(id string, path []string, onPath map[string]bool) {
		bkm, ok := bkmByID[id]
		if !ok {
			return
		}
		for _, kr := range bkm.KnowledgeRequirements {
			if kr.RequiredKnowledge == nil {
				continue
			}
			depID := kr.RequiredKnowledge.ResolvedID()
			if onPath[depID] {
				if !reported[depID] {
					reported[depID] = true
					cycle := append(append([]string{}, path...), depID)
					problems = append(problems, fmt.Sprintf(
						"business knowledge model cycle: %s", strings.Join(cycle, " -> "),
					))
				}
				continue
			}
			onPath[depID] = true
			visit(depID, append(path, depID), onPath)
			delete(onPath, depID)
		}
	}

	for _, bkm := range d.BusinessKnowledgeModels {
		visit(bkm.ID, []string{bkm.ID}, map[string]bool{bkm.ID: true})
	}

	return problems
}

func validateDecisionTable(decisionName string, tableIndex int, dt DecisionTable) []string {
	hitPolicy := dt.HitPolicy
	if hitPolicy == "" {
		hitPolicy = HitPolicyUnique
	}
	if hitPolicy != HitPolicyUnique {
		return nil
	}

	var problems []string
	for i := 0; i < len(dt.Rules); i++ {
		for j := i + 1; j < len(dt.Rules); j++ {
			if rulesOverlap(dt, dt.Rules[i], dt.Rules[j]) {
				problems = append(problems, fmt.Sprintf(
					"decision %q, decision table %d: rules %d and %d can both match the same input under hit policy UNIQUE",
					decisionName, tableIndex+1, i+1, j+1,
				))
			}
		}
	}
	return problems
}

// rulesOverlap reports whether two rules of the same decision table could
// both match a single input, by checking, column by column, whether their
// input entries' value sets intersect. Each input column is assumed to
// constrain an independent input expression, so the rules overlap overall
// iff every column overlaps individually.
func rulesOverlap(dt DecisionTable, a, b Rule) bool {
	for col := range dt.Inputs {
		var at, bt string
		if col < len(a.InputEntries) {
			at = strings.TrimSpace(a.InputEntries[col].Text)
		}
		if col < len(b.InputEntries) {
			bt = strings.TrimSpace(b.InputEntries[col].Text)
		}

		if !entriesOverlap(at, bt) {
			return false
		}
	}
	return true
}

// entriesOverlap reports whether two decision table input entries' value
// sets can intersect. It tries numeric range/comparison analysis first,
// falls back to discrete literal set comparison, and defaults to "no
// overlap" for anything it can't confidently parse.
func entriesOverlap(a, b string) bool {
	if isWildcardEntry(a) || isWildcardEntry(b) {
		return true
	}
	if a == b {
		return true
	}
	if strings.Contains(a, "not(") || strings.Contains(b, "not(") {
		return false
	}

	if aIntervals, ok := parseNumericAlternatives(a); ok {
		if bIntervals, ok := parseNumericAlternatives(b); ok {
			return numericOverlap(aIntervals, bIntervals)
		}
	}

	aTokens, aOK := parseDiscreteAlternatives(a)
	bTokens, bOK := parseDiscreteAlternatives(b)
	if aOK && bOK {
		return discreteOverlap(aTokens, bTokens)
	}

	return false
}

func isWildcardEntry(s string) bool {
	return s == "" || s == "-"
}

// splitAlternatives splits a decision table cell into its comma-separated
// unary-test alternatives, ignoring commas nested inside a range literal
// ("[1..5]") or a quoted string.
func splitAlternatives(text string) []string {
	var parts []string
	depth := 0
	inQuote := false
	start := 0

	for i, r := range text {
		switch r {
		case '"':
			inQuote = !inQuote
		case '[', '(':
			if !inQuote {
				depth++
			}
		case ']', ')':
			if !inQuote {
				depth--
			}
		case ',':
			if !inQuote && depth == 0 {
				parts = append(parts, strings.TrimSpace(text[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(text[start:]))

	return parts
}

// numInterval is a numeric range, possibly unbounded on one side, used to
// represent a single comparison/range alternative ("> 5", "[1..5)", "3").
type numInterval struct {
	loInf, hiInf   bool
	lo, hi         float64
	loOpen, hiOpen bool
}

func parseNumericAlternatives(text string) ([]numInterval, bool) {
	parts := splitAlternatives(text)
	intervals := make([]numInterval, 0, len(parts))
	for _, p := range parts {
		iv, ok := parseNumericAlternative(p)
		if !ok {
			return nil, false
		}
		intervals = append(intervals, iv)
	}
	return intervals, true
}

func parseNumericAlternative(s string) (numInterval, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return numInterval{}, false
	}

	if (strings.HasPrefix(s, "[") || strings.HasPrefix(s, "(")) &&
		(strings.HasSuffix(s, "]") || strings.HasSuffix(s, ")")) {
		loOpen := s[0] == '('
		hiOpen := s[len(s)-1] == ')'
		inner := s[1 : len(s)-1]
		bounds := strings.SplitN(inner, "..", 2)
		if len(bounds) != 2 {
			return numInterval{}, false
		}
		lo, err1 := strconv.ParseFloat(strings.TrimSpace(bounds[0]), 64)
		hi, err2 := strconv.ParseFloat(strings.TrimSpace(bounds[1]), 64)
		if err1 != nil || err2 != nil {
			return numInterval{}, false
		}
		return numInterval{lo: lo, hi: hi, loOpen: loOpen, hiOpen: hiOpen}, true
	}

	for _, op := range []string{"<=", ">=", "<", ">"} {
		if !strings.HasPrefix(s, op) {
			continue
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(s[len(op):]), 64)
		if err != nil {
			return numInterval{}, false
		}
		switch op {
		case "<":
			return numInterval{loInf: true, hi: val, hiOpen: true}, true
		case "<=":
			return numInterval{loInf: true, hi: val}, true
		case ">":
			return numInterval{hiInf: true, lo: val, loOpen: true}, true
		default: // ">="
			return numInterval{hiInf: true, lo: val}, true
		}
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return numInterval{}, false
	}
	return numInterval{lo: val, hi: val}, true
}

func numericOverlap(a, b []numInterval) bool {
	for _, x := range a {
		for _, y := range b {
			if numIntervalsOverlap(x, y) {
				return true
			}
		}
	}
	return false
}

func numIntervalsOverlap(a, b numInterval) bool {
	return !numIntervalBefore(a, b) && !numIntervalBefore(b, a)
}

// numIntervalBefore reports whether every value in a is strictly less than
// every value in b, i.e. the two intervals can't possibly overlap in this
// direction.
func numIntervalBefore(a, b numInterval) bool {
	if a.hiInf || b.loInf {
		return false
	}
	if a.hi < b.lo {
		return true
	}
	if a.hi == b.lo && (a.hiOpen || b.loOpen) {
		return true
	}
	return false
}

// parseDiscreteAlternatives splits a cell into literal tokens (quoted
// strings, bare names, booleans). It refuses anything containing
// parentheses, since that's most likely a function call or other
// expression it can't safely reason about.
func parseDiscreteAlternatives(text string) ([]string, bool) {
	parts := splitAlternatives(text)
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, "()") {
			return nil, false
		}
		tokens = append(tokens, p)
	}
	return tokens, true
}

func discreteOverlap(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, t := range a {
		set[t] = struct{}{}
	}
	for _, t := range b {
		if _, ok := set[t]; ok {
			return true
		}
	}
	return false
}
