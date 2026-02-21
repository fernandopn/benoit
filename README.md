# benoit

Minimal terminal chat client for the OpenAI Responses API built with the
official Go SDK.

## Run

- `OPENAI_API_KEY=... go run .`
- `OPENAI_API_KEY=... MATON_API_KEY=... go run .`
- `OPENAI_API_KEY=... TELEGRAM_API_KEY=... go run . -tui telegram`

## Usage

- Type a line after the `>: ` prompt.
- Type `/compress` (or `/compress <max_words>`) to compact and re-seed context.
- Submit `/exit` or `/quit` to leave.

## Flags

- `-model`
  - default: `gpt-5.2`
- `-timeout` request timeout (for example: `45s`, `2m`)
  - default: `20m`
- `-fs-root`
  - filesystem root passed to filesystem-backed tools
  - default: current working directory
- `-db-path`
  - enable sqlite logging of conversation messages
- `-bypass-compression-barrier`
  - disable compression barrier middleware
  - default: `false`
- `-tui`
  - interface mode (`simple`, `bubbletea`, or `telegram`)
  - default: `simple`
- `-telegram-poll-timeout`
  - `getUpdates` long-poll timeout in seconds (used when `-tui telegram`)
  - default: `30`
- `-telegram-allowed-users`
  - comma-separated Telegram user IDs accepted in Telegram mode
  - default: `8230557735`

## Behavior notes

- Credentials are loaded in `main.go` during startup (`OPENAI_API_KEY`, optional `MATON_API_KEY`, and `TELEGRAM_API_KEY` when `-tui telegram`).
- Tools always enabled: `code_interpreter`, `web_search`.
- `maton_gcalendar` and `maton_gmail` are enabled only when `MATON_API_KEY` is set.
- `maton_gcalendar` `list_events` requires `query.timeMin` and `query.timeMax` (RFC3339) to keep event queries bounded.
- When no TTY is detected for stdin/stdout, the app automatically uses
  simple line-based behavior.
- Storage errors are surfaced into the chat stream as `MsgTypeError` events
  while preserving normal response messages.
- Telegram mode requires `TELEGRAM_API_KEY` for bot authentication.
- Telegram session history is isolated per chat when the provider supports session-aware routing.
