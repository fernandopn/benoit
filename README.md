# benoid

Minimal terminal chat client for the OpenAI Responses API built with the
official Go SDK.

## Run

- `go run .`
- `OPENAI_API_KEY=... go run .`

## Usage

- Type a line after the `you>` prompt.
- Submit `/exit` or `/quit` to leave.

## Flags

- `-model`
  - default: `gpt-5.2`
- `-timeout` request timeout (for example: `45s`, `2m`)
  - default: `60s`
- `-fs-root`
  - filesystem root passed to filesystem-backed tools
  - default: current working directory
- `-db-path`
  - enable sqlite logging of conversation messages
- `-simple`
  - force simple line-based interface (no bubbletea UI)
- `-no-tools`
  - disable tools entirely
- `-tools`
  - comma-separated allowlist when tools are enabled
  - options: `clock`, `list_files`, `get_current_directory`, `read_file`
  - default: all

## Behavior notes

- Tool-backed startup is skipped unless tools are enabled.
- When no TTY is detected for stdin/stdout, the app automatically uses
  `-simple` behavior.
- Storage errors are surfaced into the chat stream as `MsgTypeError` events
  while preserving normal response messages.
