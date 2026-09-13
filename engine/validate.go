package engine

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ValidateDefinitions performs static, deploy-time checks that go beyond
// what XML parsing enforces: decision tables whose hit policy the DMN spec
// says can fail at evaluation time (more than one rule matching under
// UNIQUE, conflicting outputs under ANY, an output outside its declared
// values under PRIORITY/OUTPUT ORDER, a malformed COLLECT aggregation) are
// checked up front, before the table is ever evaluated.
//
// The overlap analysis only reasons about numeric ranges/comparisons and
// discrete literal sets (the common cases). Entries it can't confidently
// analyze - FEEL function calls, "not(...)" tests, arbitrary expressions -
// are treated as non-overlapping for that column rather than flagged, so
// unusual but valid tables aren't rejected on a false positive. Output
// literals are held to the same standard: only entries that are plainly a
// quoted string, number, boolean, or bare name are checked against a
// declared output values list.
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

	label := fmt.Sprintf("decision %q, decision table %d", decisionName, tableIndex+1)

	var problems []string
	switch hitPolicy {
	case HitPolicyUnique, HitPolicyAny:
		problems = append(problems, validateOverlaps(label, hitPolicy, dt)...)
	case HitPolicyPriority, HitPolicyOutputOrder:
		problems = append(problems, validateOutputValues(label, dt)...)
	case HitPolicyCollect:
		problems = append(problems, validateCollectAggregation(label, dt)...)
	}
	return problems
}

// validateOverlaps flags pairs of rules that can both match the same
// input. Under UNIQUE that's always an error (the spec requires at most
// one match); under ANY it's only an error if the overlapping rules
// disagree on output, since ANY permits overlap as long as every matching
// rule agrees.
func validateOverlaps(label, hitPolicy string, dt DecisionTable) []string {
	var problems []string
	for i := 0; i < len(dt.Rules); i++ {
		for j := i + 1; j < len(dt.Rules); j++ {
			if !rulesOverlap(dt, dt.Rules[i], dt.Rules[j]) {
				continue
			}
			switch hitPolicy {
			case HitPolicyUnique:
				problems = append(problems, fmt.Sprintf(
					"%s: rules %d and %d can both match the same input under hit policy UNIQUE",
					label, i+1, j+1,
				))
			case HitPolicyAny:
				if !rulesHaveSameOutputs(dt.Rules[i], dt.Rules[j]) {
					problems = append(problems, fmt.Sprintf(
						"%s: rules %d and %d can both match the same input but produce different outputs, which hit policy ANY forbids",
						label, i+1, j+1,
					))
				}
			}
		}
	}
	return problems
}

// rulesHaveSameOutputs reports whether two rules' output entries are
// textually identical, column by column.
func rulesHaveSameOutputs(a, b Rule) bool {
	if len(a.OutputEntries) != len(b.OutputEntries) {
		return false
	}
	for i := range a.OutputEntries {
		if strings.TrimSpace(a.OutputEntries[i].Text) != strings.TrimSpace(b.OutputEntries[i].Text) {
			return false
		}
	}
	return true
}

// validateOutputValues flags rule output entries that aren't among their
// column's declared output values list, for PRIORITY and OUTPUT ORDER hit
// policies, where the declared list is what defines the priority ordering
// (and, at evaluation time, an output outside it just sorts last rather
// than erroring - so this is the only place such a mistake gets caught).
func validateOutputValues(label string, dt DecisionTable) []string {
	var problems []string
	for oi, out := range dt.Output {
		values, ok := parseOutputValueList(out.OutputValues.Text)
		if !ok {
			continue
		}

		colDesc := out.Name
		if colDesc == "" {
			colDesc = fmt.Sprintf("output column %d", oi+1)
		}

		for ri, rule := range dt.Rules {
			if oi >= len(rule.OutputEntries) {
				continue
			}
			text := strings.TrimSpace(rule.OutputEntries[oi].Text)
			if isWildcardEntry(text) {
				continue
			}
			lit, ok := parseOutputLiteral(text)
			if !ok {
				continue
			}
			if !slices.Contains(values, lit) {
				problems = append(problems, fmt.Sprintf(
					"%s: rule %d's %s is %q, which isn't in its declared output values (%s)",
					label, ri+1, colDesc, lit, strings.Join(values, ", "),
				))
			}
		}
	}
	return problems
}

// parseOutputValueList parses an output column's declared outputValues
// text (a comma-separated list of literals) into its individual values. It
// returns ok=false for an empty/undeclared list, or if any entry isn't a
// literal it can confidently parse - in which case callers should skip
// validation rather than risk a false positive.
func parseOutputValueList(text string) ([]string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}

	parts := splitAlternatives(text)
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		lit, ok := parseOutputLiteral(strings.TrimSpace(p))
		if !ok {
			return nil, false
		}
		values = append(values, lit)
	}
	return values, true
}

// parseOutputLiteral reports the plain value of a decision table output
// entry - a quoted string unquoted, or a number/boolean/bare name as-is -
// when it's simple enough to compare with confidence. Anything else
// (function calls, expressions) returns ok=false.
func parseOutputLiteral(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	if unquoted, err := strconv.Unquote(s); err == nil {
		return unquoted, true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return s, true
	}
	if s == "true" || s == "false" || s == "null" {
		return s, true
	}
	if bareNameEntry.MatchString(s) {
		return s, true
	}
	return "", false
}

// validateCollectAggregation flags a COLLECT hit policy's aggregation
// attribute if it isn't one of the spec's recognized values, or if a
// numeric aggregation (SUM/MIN/MAX) is paired with more than one output
// column - evaluation can't reduce a per-rule record to a single number, so
// it silently falls back to returning the raw list instead of aggregating.
func validateCollectAggregation(label string, dt DecisionTable) []string {
	switch dt.Aggregation {
	case "", AggregationSum, AggregationMin, AggregationMax, AggregationCount:
	default:
		return []string{fmt.Sprintf(
			"%s: unrecognized COLLECT aggregation %q (expected SUM, MIN, MAX, COUNT, or none)",
			label, dt.Aggregation,
		)}
	}

	if len(dt.Output) > 1 {
		switch dt.Aggregation {
		case AggregationSum, AggregationMin, AggregationMax:
			return []string{fmt.Sprintf(
				"%s: COLLECT aggregation %s needs a single numeric output column, but this table has %d",
				label, dt.Aggregation, len(dt.Output),
			)}
		}
	}
	return nil
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
