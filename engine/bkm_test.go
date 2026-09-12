package engine

import "testing"

// TestBkmFuncSelfCycleDoesNotOverflow guards against the crash described in
// FINDINGS.md: building the native call scope for a BKM whose
// knowledgeRequirement (directly or transitively) points back at itself used
// to recurse without bound, which is a fatal, unrecoverable Go stack
// overflow rather than an ordinary error. bkmFuncSeen must terminate instead.
func TestBkmFuncSelfCycleDoesNotOverflow(t *testing.T) {
	bkm := BusinessKnowledgeModel{
		ID:                    "bkm_a",
		Name:                  "A",
		Variable:              Variable{Name: "A"},
		KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_a")},
	}
	bkmMap := map[string]BusinessKnowledgeModel{"bkm_a": bkm}

	fn := bkmFunc(bkm, bkmMap, map[string]ItemDefinition{})
	if fn == nil {
		t.Fatal("expected a non-nil function even for a self-referencing BKM")
	}
}

// TestBkmFuncTransitiveCycleDoesNotOverflow is the same guard for a longer
// cycle (a -> b -> a) rather than a direct self-reference.
func TestBkmFuncTransitiveCycleDoesNotOverflow(t *testing.T) {
	a := BusinessKnowledgeModel{
		ID:                    "bkm_a",
		Name:                  "A",
		Variable:              Variable{Name: "A"},
		KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_b")},
	}
	b := BusinessKnowledgeModel{
		ID:                    "bkm_b",
		Name:                  "B",
		Variable:              Variable{Name: "B"},
		KnowledgeRequirements: []KnowledgeRequirement{requiresBKM("bkm_a")},
	}
	bkmMap := map[string]BusinessKnowledgeModel{"bkm_a": a, "bkm_b": b}

	fn := bkmFunc(a, bkmMap, map[string]ItemDefinition{})
	if fn == nil {
		t.Fatal("expected a non-nil function even for a transitively cyclical BKM")
	}
}
