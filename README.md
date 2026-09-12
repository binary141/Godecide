# dmn

A [DMN](https://www.omg.org/spec/DMN/) (Decision Model and Notation) engine
written in Go, plus an HTTP API, a browser-based table builder, and a Go SDK
for driving it.

It parses DMN 1.3/1.4/1.5 XML, evaluates decisions and decision tables using
[FEEL](https://github.com/binary141/FEEL.go), and validates decision graphs
(BKM cycle detection, overlapping `UNIQUE` hit-policy rules) before letting
them run.

## Features

- **DMN parsing & evaluation** — decision tables, literal expressions,
  business knowledge models (BKMs), and chained decisions (DRGs), evaluated
  in topological order.
- **Hit policies** — `UNIQUE`, `FIRST`, `ANY`, `PRIORITY`, `COLLECT`
  (including `SUM`/`MIN`/`MAX`/`COUNT` aggregators), and `RULE ORDER`.
- **Validation** — deploy-time checks for provably overlapping rules in
  `UNIQUE` tables and circular BKM requirements.
- **Evaluation timeouts** — evaluation runs under a context deadline so a
  malformed or runaway model (e.g. an unbounded loop) can't hang a request.
- **HTTP server** (`cmd/server`) — a Gin-based API for evaluating ad hoc
  decision graphs, exporting them to DMN XML, and deploying/versioning DMN
  files backed by Postgres.
- **Web UI** (`cmd/server/web`) — a browser table builder served alongside
  the API, for building and testing decision tables without hand-writing
  XML.
- **Go SDK** (`sdk`) — a typed HTTP client for the server's API.
- **TCK compliance suite** (`tests`) — generated tests against the
  [DMN TCK](https://github.com/dmn-tck/tck) test cases.

## Project layout

```
engine/         DMN XML parsing, model, evaluation, hit policies, validation
db/             Postgres connection, migrations, deployment/evaluation storage
deployments/    HTTP handlers for deploying and evaluating stored DMN files
cmd/server/     HTTP server entrypoint + embedded web UI (table builder)
sdk/            Go client for the HTTP API
tests/          Generated tests against the DMN TCK
testdata/tck/   DMN TCK test cases (git submodule)
```

## Getting started

### Prerequisites

- Go 1.26+
- Docker (for Postgres, used by the server/deployments API)

### Clone

This repo uses the [DMN TCK](https://github.com/dmn-tck/tck) as a git
submodule for compliance tests:

```sh
git clone --recurse-submodules <repo-url>
# or, if already cloned:
git submodule update --init
```

### Run the engine standalone

```sh
go run . [path/to/file.dmn]
```

Defaults to a sample TCK file if no path is given.

### Run the HTTP server + web UI

Bring up Postgres and the API together:

```sh
cp .env.example .env
make up-d
```

Or run the server directly against a local Postgres (see `.env.example` for
the expected `POSTGRES_*` variables):

```sh
go run ./cmd/server
```

The server listens on `:8080` by default (`-addr` to override) and serves
the table builder UI at `/` alongside the JSON API:

| Method | Path                                    | Description                          |
|--------|------------------------------------------|---------------------------------------|
| GET    | `/healthz`                                | Health check                          |
| POST   | `/api/evaluate`                           | Evaluate an ad hoc decision graph     |
| POST   | `/api/export`                             | Export a decision graph to DMN XML    |
| POST   | `/api/deployments`                        | Deploy a DMN XML file                 |
| GET    | `/api/deployments`                        | List deployments                      |
| GET    | `/api/deployments/latest`                 | Get the latest deployment by name     |
| GET    | `/api/deployments/:deploymentId`          | Get a deployment                      |
| DELETE | `/api/deployments/:deploymentId`          | Delete a deployment                   |
| POST   | `/api/deployments/:deploymentId/evaluate` | Evaluate inputs against a deployment  |
| GET    | `/api/deployments/:deploymentId/evaluations` | Evaluation history for a deployment |

### Using the Go SDK

```go
import "dmn/sdk"

client := sdk.New(sdk.DefaultBaseURL)

deployment, err := client.CreateDeployment(ctx, dmnXML)
outputs, err := client.EvaluateDeployment(ctx, deployment.ID, map[string]any{
    "Full Name": "John",
})
```

## Testing

```sh
make test
```

Runs the full suite (unit tests plus the generated TCK compliance tests
under `tests/`) and prints a pass/fail summary.

## Roadmap

See [ROADMAP.md](ROADMAP.md) for planned work, phased from initial project
setup through platform parity (auth, multi-tenancy, etc.).
