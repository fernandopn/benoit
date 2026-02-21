# benoit

Minimal terminal chat client for the OpenAI Responses API built with the
official Go SDK.

## Run

- `OPENAI_API_KEY=... go run . tui --render simple`
- `OPENAI_API_KEY=... MATON_API_KEY=... go run . tui --render bubbletea`
- `OPENAI_API_KEY=... TELEGRAM_API_KEY=... go run . channel_listener --channel telegram`
- `go run . list_sessions`

## Usage

- Type a line after the `>: ` prompt.
- Type `/compress` (or `/compress <max_words>`) to compact and re-seed context.
- Submit `/exit` or `/quit` to leave.

## Commands and flags

### `tui`

- `-model`
  - default: `gpt-5.2`
- `-timeout` request timeout (for example: `45s`, `2m`)
  - default: `20m`
- `-fs-root`
  - filesystem root passed to filesystem-backed tools
  - default: current working directory
- `-db-path`
  - database path used for both provider trace logging and per-session provider state
  - default: `db.sqlite`
- `-bypass-compression-barrier`
  - disable compression barrier middleware
  - default: `false`
- `--render`
  - interface mode (`simple` or `bubbletea`)
  - default: `simple`
- `--session-id`
  - resume an existing session ID

### `channel_listener`

- `--channel`
  - channel listener type (`telegram`)
- `-model`
  - default: `gpt-5.2`
- `-timeout` request timeout (for example: `45s`, `2m`)
  - default: `20m`
- `-fs-root`
  - reserved for shared config; filesystem tools are enabled only in `tui`
  - default: current working directory
- `-db-path`
  - database path used for both provider trace logging and per-session provider state
  - default: `db.sqlite`
- `-bypass-compression-barrier`
  - disable compression barrier middleware
  - default: `false`
- `-telegram-poll-timeout`
  - `getUpdates` long-poll timeout in seconds
  - default: `30`
- `-telegram-allowed-users`
  - comma-separated Telegram user IDs accepted in Telegram mode
  - default: `8230557735`

### `list_sessions`

- `-db-path`
  - database path used for both provider trace logging and per-session provider state
  - default: `db.sqlite`
- `-fs-root`
  - reserved for shared config; not used by `list_sessions`
  - default: current working directory

## Behavior notes

- Credentials are loaded in `main.go` during startup: `OPENAI_API_KEY` (required for provider commands), `MATON_API_KEY` (optional), and `TELEGRAM_API_KEY` (required for `channel_listener --channel telegram`, optional otherwise to enable channel messaging tools).
- Tools always enabled in provider commands: `code_interpreter`, `web_search`, `get_time`.
- TUI mode also enables filesystem tools scoped by `-fs-root`: `list_files`, `get_current_directory`, `read_file`.
- `send_channel_message` is enabled only in `tui` mode when `TELEGRAM_API_KEY` is set. It accepts `channel`, `user_id`, and `message`.
- `maton_gcalendar` and `maton_gmail` are enabled only when `MATON_API_KEY` is set.
- `maton_gcalendar` `list_events` requires `query.timeMin` and `query.timeMax` (RFC3339) to keep event queries bounded.
- When no TTY is detected for stdin/stdout, the app automatically uses
  simple line-based behavior.
- Storage errors are surfaced into the chat stream as `MsgTypeError` events
  while preserving normal response messages.
- Database persistence and trace logging are handled via Bun models and query builders.
- Telegram mode requires `TELEGRAM_API_KEY` for bot authentication.
- Telegram session history is isolated per user with session IDs in the format `telegram:<telegram_user_id>`.
