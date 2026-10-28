# Swarg

A light terminal coding agent for local LLMs. Single Go binary, talks to any
OpenAI-compatible server (for example `mlx_lm.server` or llama.cpp at
`http://127.0.0.1:8080/v1`).

Status: early, work in progress.

## Features

- Full-screen terminal UI; type while the agent works, messages are queued between tool calls.
- Built-in tools: shell (with background jobs), read, write, edit, grep, glob, web search and fetch.
- Task mode: the agent can run a sub-task in a clean context and return only its result (nested).
- Sessions saved per project, resume with `--continue` / `--resume`; `/` commands, model switch.
- Context economy and compaction tuned for small models; guards against repeat and cycle loops.
- Runs on macOS, Linux and Windows.

## Build

Requires Go 1.26+.

```sh
cd App
go build -o swarg ./cmd/swarg
./swarg --base-url http://127.0.0.1:8080/v1 --model <model-name>
```

`swarg --help` lists all flags.

## Development

```sh
git config core.hooksPath Tools/git-hooks   # pre-commit gate: gofmt, vet, tests, cross builds
cd App && go test -race ./...
```

- `App/features/` - BDD scenarios (godog), run from `go test`.
- `App/tests/sandbox/` - e2e runs in an isolated container (Docker / Colima) against a real model.
- `App/tests/eval/` - agent evaluation suite (21 tasks).
- `Docs/Domain.md` - glossary, rules and invariants; `Docs/Modules/` - one file per package.

## License

Apache License 2.0, see `LICENSE` and `NOTICE`.
