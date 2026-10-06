# Running the DMN TCK

How to reproduce the results submitted to the [DMN TCK](https://github.com/dmn-tck/tck).
The TCK is vendored as the `testdata/tck` git submodule, and the tests are
generated from it by `cmd/gentests` (see `cmd/gentests/README.md` for details).

## Requirements

- Go (version in `go.mod`)
- git
- make

## Steps

```sh
git clone git@github.com:binary141/Godecide.git && cd Godecide

# 1. Fetch the TCK submodule, at the commit this repo pins.
git submodule update --init

# 2. (Optional) Update to the latest TCK test cases.
git submodule update --remote testdata/tck

# 3. Generate the Go tests into ./tests from the TCK test cases.
go generate ./...

# 4. Run the TCK suite.
make tck-test
```

Step 2 moves the submodule pointer, so results will differ from the pinned
version; skip it to reproduce a submitted result exactly. Always re-run step 3
after changing the submodule.

## Output

`make tck-test` prints the Go test log and a summary line
(`TCK results: N passed, N failed, N skipped`), and writes two files to the repo
root in the format the TCK expects for submissions:

| File | Contents |
| --- | --- |
| `tck_results.csv` | one row per test case: `"<dir>","<test file>","<case id>","SUCCESS\|ERROR\|IGNORED","<message>"` |
| `tck_results.properties` | vendor and product metadata |

Override the metadata when running, e.g.
`make tck-test TCK_VENDOR_NAME="Acme" TCK_PRODUCT_VERSION=1.0.0`
(see the `TCK_*` variables at the top of the `Makefile`).

## Submitting

Copy both files to `TestResults/<vendor>/<version>/` in a fork of
[dmn-tck/tck](https://github.com/dmn-tck/tck) and open a pull request.
