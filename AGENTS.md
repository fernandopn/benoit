# Benoit

Minimal terminal chat client for the OpenAI Responses API using the official Go SDK. It reads a line at a time from stdin, streams the assistant response, and keeps multi-turn context by reusing the previous response ID.

## Requirements
- Go 1.25+
- `OPENAI_API_KEY` environment variable set to a valid key
- Optional integrations:
  - `MATON_API_KEY` enables Maton tools
  - `TELEGRAM_API_KEY` is required only for `-tui telegram`

## Build
1. Fetch dependencies:
   - `go mod tidy`
2. Build:
   - `go build ./...`
3. Always run `go fmt ./...` after code changes.
4. Always run `go test ./...` after code changes.
5. Always run a build after making code changes to verify it still compiles.

## Conventions
- Libraries should not create their own contexts. If a context is needed, accept it as an argument and let callers decide timeouts/cancellation.
- Avoid setting default values when an input is empty; defaults should be handled by the caller or flags.
- Prefer simple, direct solutions. Avoid adding extra behaviors or placeholders that were not requested, especially if they may be removed later.
- Within a package, the category is set by the file name (e.g., `openai.go`). Keep all OpenAI-related code in `openai.go` and tests in `openai_test.go`. Implementation-agnostic interfaces or shared structs can live in `base.go` (e.g., `tools/base.go`).
- Credential boundary: only `main.go` should read environment variables. Library packages must accept explicit credentials/config through constructors or parameters.
- Do not add `NewXFromEnv` constructors outside the `main` package.
- When changing tool registration, update `README.md` and related tests in the same change to prevent drift.
- Keep `main.go` as a thin entrypoint: parse config/credentials, then delegate orchestration to internal packages.
- Prefer weak decoupling via explicit seams (interfaces and injected builders/factories) over direct concrete construction across packages.
- Prefer a `run() error` pattern with a single `os.Exit` call in `main` to keep cleanup paths reliable.
- Do not hardcode policy/account IDs in runtime logic; expose them through flags or explicit config.
- Keep `.gitignore` updated for generated/runtime artifacts introduced by features.

If your Go module cache is not writable, use a custom cache:
- `GOMODCACHE=/tmp/gomodcache go mod tidy`
- `GOMODCACHE=/tmp/gomodcache go build ./...`

## Run
- `go run .`

## Usage
- Type a message after the `>: ` prompt.
- Type `/compress` (or `/compress <max_words>`) to compact and re-seed context.
- Type `/exit` or `/quit` to leave.
