# gentests

Generates Go tests for the engine from the [DMN TCK](https://github.com/dmn-tck/tck)
(Technology Compatibility Kit), which is vendored as the `testdata/tck` git
submodule.

See [TCK.md](../../TCK.md) for the end-to-end steps.

## Usage

From the repo root:

```sh
git submodule update --init   # once, to fetch testdata/tck
go generate ./...             # runs `go run ./cmd/gentests` (see //go:generate in main.go)
make tck-test                 # run the generated suite
```

Output goes to `tests/`. Every run first deletes the old `tests/generated_tck_*.go`
files, so the directory is always an exact reflection of the current TCK data.
Don't edit the generated files; change the generator and re-run it.

## What it generates

It walks `testdata/tck/TestCases/compliance-level-2` and `compliance-level-3`
(level 1 is not generated). In each folder it pairs the model with its test XML:

- **Model**: the `.dmn` named after the folder (`<folder>.dmn`). If there isn't
  one, the last `.dmn` file alphabetically is used. The preference matters for
  folders that also hold imported models, such as `0086-import`.
- **Test cases**: every `*-test-*.xml` file in the folder.

For each `<testCase>` it emits one Go test, `TestTCK_<folder>_<caseID>`, which:

1. builds the input map from the `<inputNode>` elements,
2. evaluates the wanted decisions (`EvaluateDecisions`), or calls
   `EvaluateService` for `type="decisionService"` cases,
3. asserts each `<resultNode>`'s expected scalar, list or structure.

Output files:

| File | Contents |
| --- | --- |
| `tests/generated_tck_<folder>_test.go` | one file per TCK folder |
| `tests/generated_tck_helpers_test.go` | shared helpers: `mustParse` (memoized `engine.ParseFile`, so imports resolve) and `mustFeelValue` for date/time/duration literals |

Result nodes are matched to decisions by name, using the decision IDs from the
model.

## Submission results

`make tck-test` also writes `tck_results.csv` in the repo root, in the format the
dmn-tck repo expects under `TestResults/<vendor>/<version>/`: one row per test
case, `"<dir>","<test file>","<case id>","SUCCESS|ERROR|IGNORED","<message>"`.
Skipped tests are `IGNORED`. It's driven by the `TCK_RESULTS_CSV` env var, which
the generated `TestMain` reads. `tck_results.properties` is written alongside it (override the `TCK_*` variables in the Makefile for vendor/product details).

## Skipped tests

Some cases are still emitted, as `t.Skip`, so they show up in the totals:

- `0076-feel-external-java`: Java external functions aren't supported.
- Cases where no result node maps to a decision in the chosen model.

## Warnings

The generator prints `warn:` lines to stderr and carries on:

- `no decision for result "X" in <file>`: the test XML asks for a result that the
  chosen model doesn't define. This usually means the wrong `.dmn` was picked for
  the folder, or the name is only available through an import.
- `parse dmn ...` / `parse xml ...`: a file couldn't be parsed and was skipped.

The final line (`wrote N files, M tests`) is the summary.
