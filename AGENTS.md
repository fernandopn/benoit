# Benoid

Minimal terminal chat client for the OpenAI Responses API using the official Go SDK. It reads a line at a time from stdin, streams the assistant response, and keeps multi-turn context by reusing the previous response ID.

## Requirements
- Go 1.25+
- `OPENAI_API_KEY` environment variable set to a valid key

## Build
1. Fetch dependencies:
   - `go mod tidy`
2. Build:
   - `go build ./...`
3. Always run a build after making code changes to verify it still compiles.

If your Go module cache is not writable, use a custom cache:
- `GOMODCACHE=/tmp/gomodcache go mod tidy`
- `GOMODCACHE=/tmp/gomodcache go build ./...`

## Run
- `go run .`

## Usage
- Type a message after `you>`
- Type `/exit` or `/quit` to leave
