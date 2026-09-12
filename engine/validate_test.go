package engine

import "testing"

func TestEntriesOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"wildcard vs anything", "-", "\"whatever\"", true},
		{"identical text", "> 5", "> 5", true},
		{"numeric ranges overlap", "< 10", "<= 5", true},
		{"numeric ranges disjoint", "< 10", ">= 10", false},
		{"numeric ranges disjoint open bound", "< 10", ">= 11", false},
		{"bracket ranges overlap", "[1..5]", "[4..8]", true},
		{"bracket ranges disjoint", "[1..5]", "(5..8]", false},
		{"bracket ranges touch inclusive", "[1..5]", "[5..8]", true},
		{"bare numbers equal", "5", "5", true},
		{"bare numbers different", "5", "6", false},
		{"discrete strings overlap", "\"A\", \"B\"", "\"B\", \"C\"", true},
		{"discrete strings disjoint", "\"A\"", "\"B\"", false},
		{"not() is conservative", "not(\"A\")", "\"A\"", false},
		{"unparseable function calls are conservative", "someFunc(1)", "someFunc(2)", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := entriesOverlap(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("entriesOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func numberInput(name string) Input {
	return Input{
		InputExpression: InputExpression{Text: name, TypeRef: "number"},
	}
}

func TestValidateDecisionTableUniqueOverlap(t *testing.T) {
	dt := DecisionTable{
		HitPolicy: HitPolicyUnique,
		Inputs:    []Input{numberInput("Age")},
		Output:    []Output{{Name: "Result"}},
		Rules: []Rule{
			{InputEntries: []InputEntry{{Text: "< 18"}}, OutputEntries: []OutputEntry{{Text: "\"minor\""}}},
			{InputEntries: []InputEntry{{Text: "<= 20"}}, OutputEntries: []OutputEntry{{Text: "\"young\""}}},
		},
	}

	problems := validateDecisionTable("Eligibility", 0, dt)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %v", len(problems), problems)
	}
}

func TestValidateDecisionTableUniqueNoOverlap(t *testing.T) {
	dt := DecisionTable{
		HitPolicy: HitPolicyUnique,
		Inputs:    []Input{numberInput("Age")},
		Output:    []Output{{Name: "Result"}},
		Rules: []Rule{
			{InputEntries: []InputEntry{{Text: "< 18"}}, OutputEntries: []OutputEntry{{Text: "\"minor\""}}},
			{InputEntries: []InputEntry{{Text: ">= 18"}}, OutputEntries: []OutputEntry{{Text: "\"adult\""}}},
		},
	}

	problems := validateDecisionTable("Eligibility", 0, dt)
	if len(problems) != 0 {
		t.Fatalf("expected no problems, got %v", problems)
	}
}

func TestValidateDecisionTableSkipsNonUniquePolicies(t *testing.T) {
	dt := DecisionTable{
		HitPolicy: HitPolicyFirst,
		Inputs:    []Input{numberInput("Age")},
		Output:    []Output{{Name: "Result"}},
		Rules: []Rule{
			{InputEntries: []InputEntry{{Text: "< 18"}}, OutputEntries: []OutputEntry{{Text: "\"minor\""}}},
			{InputEntries: []InputEntry{{Text: "<= 20"}}, OutputEntries: []OutputEntry{{Text: "\"young\""}}},
		},
	}

	problems := validateDecisionTable("Eligibility", 0, dt)
	if len(problems) != 0 {
		t.Fatalf("expected no problems for FIRST hit policy, got %v", problems)
	}
}

func TestValidateDefinitions(t *testing.T) {
	def := Definitions{
		Decisions: []Decision{
			{
				Name: "Eligibility",
				DecisionTables: []DecisionTable{
					{
						HitPolicy: HitPolicyUnique,
						Inputs:    []Input{numberInput("Age")},
						Output:    []Output{{Name: "Result"}},
						Rules: []Rule{
							{InputEntries: []InputEntry{{Text: "< 18"}}, OutputEntries: []OutputEntry{{Text: "\"minor\""}}},
							{InputEntries: []InputEntry{{Text: "<= 20"}}, OutputEntries: []OutputEntry{{Text: "\"young\""}}},
						},
					},
				},
			},
		},
	}

	problems := ValidateDefinitions(def)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %v", len(problems), problems)
	}
}

func requiresBKM(id string) KnowledgeRequirement {
	return KnowledgeRequirement{RequiredKnowledge: &RequiredKnowledge{Href: "#" + id}}
}

func TestValidateBKMCyclesSelfReference(t *testing.T) {
	def := Definitions{
		BusinessKnowledgeModels: []BusinessKnowledgeModel{
			{ID: "bkm_a", Name: "A", KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_a")}},
		},
	}

	problems := validateBKMCycles(def)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %v", len(problems), problems)
	}
}

func TestValidateBKMCyclesTransitive(t *testing.T) {
	def := Definitions{
		BusinessKnowledgeModels: []BusinessKnowledgeModel{
			{ID: "bkm_a", Name: "A", KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_b")}},
			{ID: "bkm_b", Name: "B", KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_c")}},
			{ID: "bkm_c", Name: "C", KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_a")}},
		},
	}

	problems := validateBKMCycles(def)
	if len(problems) == 0 {
		t.Fatalf("expected at least 1 problem for a -> b -> c -> a cycle, got none")
	}
}

func TestValidateBKMCyclesNoFalsePositiveOnDAG(t *testing.T) {
	def := Definitions{
		BusinessKnowledgeModels: []BusinessKnowledgeModel{
			{ID: "bkm_a", Name: "A", KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_b"), requiresBKM("bkm_c")}},
			{ID: "bkm_b", Name: "B", KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_c")}},
			{ID: "bkm_c", Name: "C"},
		},
	}

	problems := validateBKMCycles(def)
	if len(problems) != 0 {
		t.Fatalf("expected no problems for a valid DAG (diamond shape), got %v", problems)
	}
}
