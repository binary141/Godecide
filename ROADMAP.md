# DMN Engine — Go Implementation Roadmap
 
---
 
## Phase 1 — Project Setup
 
- [X] Initialize Go module (`go mod init`)
---
 
## Phase 2 — Core Model
 
- [X] Define `Variable` struct
- [X] Define `LiteralExpression` struct
- [X] Define `RequiredInput` and `RequiredDecision` structs with `ResolvedID()`
- [X] Define `InformationRequirement` struct
- [X] Define `InputData` struct
- [X] Define `Decision` struct with `*LiteralExpression` (pointer, more expression types come later)
- [X] Define `Definitions` struct
- [ ] Define `ValueType` constants (`TypeString`, `TypeNumber`, `TypeBoolean`, etc.)
- [ ] Define `Value` struct with `Raw any` and typed accessors (`AsString()`, `AsNumber()`, etc.)
- [ ] Define `CoerceToValue()` that converts raw Go types into `Value`
- [ ] Write unit tests for `CoerceToValue()`
- [ ] Write unit tests for `Value` accessors
---
 
## Phase 3 — XML Parsing
 
- [X] Write `DetectVersion()` that reads the root namespace and returns a version string
- [ ] Write unit test for `DetectVersion()` with the 1.3, 1.4, and 1.5 namespace URIs
- [X] Write a `Parse([]byte) (*model.Definitions, error)` function using `encoding/xml`
- [X] Test parsing the `0001-input-data-string` TCK file into your structs
- [ ] Test parsing the `0002-input-data-number` TCK file
- [X] Confirm `DMNDI` block is silently ignored
- [ ] Confirm `ResolvedID()` correctly strips `#` from hrefs
---
 
## Phase 4 — Evaluation Context
 
- [ ] Define an `EvaluationContext` struct that holds input variables as `map[string]Value`
- [ ] Write `NewContext(inputs map[string]any) (EvaluationContext, error)` that coerces raw inputs using `CoerceToValue()`
- [ ] Write `context.Get(name string) (Value, bool)`
- [ ] Write `context.Set(name string, value Value)`
- [ ] Write unit tests for context get/set/coercion
---
 
## Phase 5 — FEEL Stub
 
- [ ] Define `Evaluator` interface with `Evaluate(expr string, ctx EvaluationContext) (Value, error)`
- [ ] Implement `StubEvaluator` that handles:
  - [ ] Empty/dash (`-`) — always matches
  - [ ] String literals (`"hello"`)
  - [ ] Exact number match (`42`)
  - [ ] Simple comparisons (`< 10`, `>= 5`, `!= 3`)
  - [ ] Simple string concatenation (`"Hello " + name`)
- [ ] Write unit tests for each stub case
- [ ] Confirm stub evaluator satisfies the `Evaluator` interface
---
 
## Phase 6 — Hit Policies
 
- [ ] Define `HitPolicy` interface with `Apply(matches []Rule) (Value, error)`
- [ ] Implement and test `UNIQUE` — error if more than one rule matches
- [ ] Implement and test `FIRST` — return first matching rule
- [ ] Implement and test `ANY` — return result if all matches agree, error if they differ
- [ ] Implement and test `COLLECT` — return all matching outputs as a list
- [ ] Implement and test `RULE ORDER` — like collect but preserves rule order
- [ ] Implement and test `PRIORITY` — return highest priority match
---
 
## Phase 7 — Decision Table Evaluation
 
- [ ] Add `DecisionTable` struct to model (inputs, outputs, rules, hit policy)
- [ ] Add `*DecisionTable` pointer to `Decision` alongside `*LiteralExpression`
- [ ] Write XML parsing for `decisionTable`, `input`, `output`, `rule`, `inputEntry`, `outputEntry`
- [ ] Write `EvaluateTable(table DecisionTable, ctx EvaluationContext) (Value, error)` that:
  - [ ] Evaluates each rule's input entries against context values
  - [ ] Collects matching rules
  - [ ] Applies the hit policy
- [ ] Test against TCK decision table files
---
 
## Phase 8 — Literal Expression Evaluation
 
- [ ] Write `EvaluateLiteralExpression(expr LiteralExpression, ctx EvaluationContext) (Value, error)`
- [ ] Wire it through the stub evaluator
- [ ] Test against the `0001-input-data-string` TCK file end to end:
  - Input: `{"Full Name": "John"}`
  - Expected output: `"Hello John"`
---
 
## Phase 9 — Decision Graph (DRG)
 
- [ ] Build an `index` of all decisions and input data by ID after parsing
- [ ] Write `topologicalSort(decisionID string) ([]string, error)` using the `informationRequirement` hrefs as edges
- [ ] Detect and error on circular dependencies
- [ ] Write `Engine.Evaluate(decisionID string, inputs map[string]any) (Value, error)` that:
  - [ ] Builds the context from inputs
  - [ ] Sorts the decision graph
  - [ ] Evaluates each decision in order, adding results to context
  - [ ] Returns the final decision's result
- [ ] Test a chained decision (one decision depending on another)
---
 
## Phase 10 — TCK Test Runner
 
- [ ] Clone or submodule the TCK repo into `tck/testdata/`
- [ ] Find and parse the TCK test case XML format (each test case has inputs and expected outputs)
- [ ] Write a `RunTCKTest(path string) error` function
- [ ] Write a Go test that iterates all TCK test files and runs them
- [ ] Track a pass/fail count and print a compliance summary
- [ ] Get the Phase 2 TCK tests (basic literals and input data) passing
---
 
## Phase 11 — Context Expression Type
 
- [ ] Add `Context` and `ContextEntry` structs to model
- [ ] Add XML parsing for `context` and `contextEntry`
- [ ] Add `*Context` pointer to `Decision`
- [ ] Write `EvaluateContext(ctx model.Context, evalCtx EvaluationContext) (Value, error)`
- [ ] Test against TCK context expression files
---
 
## Phase 12 — Real FEEL (the big one)
 
- [ ] Study the FEEL grammar in the DMN spec (Chapter 10)
- [ ] Write a lexer that tokenizes FEEL expressions into typed tokens
- [ ] Write unit tests for the lexer covering all token types
- [ ] Write a parser that builds an AST from tokens
- [ ] Write unit tests for the parser
- [ ] Write an AST evaluator
- [ ] Replace `StubEvaluator` with `FEELEvaluator` behind the same interface
- [ ] Expand TCK coverage
---
 
## Phase 13 — Hardening
 
- [ ] Add `conditional` expression support (DMN 1.4)
- [ ] Add `iterator` expression support (DMN 1.4)
- [ ] Add proper duration types (`YearMonthDuration`, `DayTimeDuration`)
- [ ] Add date/time type support
- [ ] Add type validation (check `typeRef` matches actual evaluated value type)
- [ ] Improve error messages (include decision name, rule number, expression text)
---
 
## Phase 14 — API (optional)
 
- [ ] Define a clean public API surface in `engine/engine.go`
- [ ] Add an HTTP handler that accepts JSON inputs and returns JSON outputs
- [ ] Add request/response logging
- [ ] Write integration tests against the HTTP API
 

