# benoit

Minimal terminal chat client built with the official OpenAI Go SDK. It supports
two backends selected with `-provider`:

- `openai` (default): the OpenAI Responses API, which chains turns server-side
  with a response id.
- `openrouter`: the OpenAI-compatible Chat Completions API at
  `https://openrouter.ai/api/v1`. OpenRouter is stateless, so the full
  conversation history is kept locally and resent on each request.

## Run

- `OPENAI_API_KEY=... go run . tui --render simple`
- `OPENAI_API_KEY=... MATON_API_KEY=... go run . tui --render bubbletea`
- `OPENROUTER_API_KEY=... go run . tui -provider openrouter` (defaults to `z-ai/glm-5.1`)
- `go run . tui --env-file .env --render simple`
- `go run . ssh --env-file .env`
- `OPENAI_API_KEY=... SSH_ALLOWED_PUBLIC_KEYS="<key1>,<key2>" go run . ssh`
- `OPENAI_API_KEY=... TELEGRAM_API_KEY=... TELEGRAM_ALLOWED_USERS="123,456" go run . channel_listener --channel telegram`
- `go run . list_sessions`

## Usage

- Type a line after the `>: ` prompt.
- Type `/compact` (or `/compact <max_words>`) to compact and re-seed context.
- Submit `/exit` or `/quit` to leave.

## Commands and flags

### `tui`

- `-model`
  - default: `gpt-5.5` for `-provider openai`, `z-ai/glm-5.1` for `-provider openrouter`
- `-provider`
  - LLM provider: `openai` or `openrouter`
  - default: `openai`
- `-timeout` request timeout (for example: `45s`, `2m`)
  - default: `20m`
- `-fs-root`
  - filesystem sandbox root for file tools (`glob`, `grep`, `read`, `write`, `apply_patch`)
  - when configured, file tools run in a chroot-like view where this directory is virtual `/`
  - if omitted, file tools are not registered
  - default: current working directory
- `-db-path`
  - database path used for both provider trace logging and per-session provider state
  - default: `db.sqlite`
- `-env-file`
  - optional `.env` file path loaded before reading process environment variables
  - values from this file take precedence over process environment values
  - if the file does not exist, startup continues normally
  - default: `.env`
- `-bypass-compression-barrier`
  - disable compression barrier middleware
  - default: `false`
- `--render`
  - interface mode (`simple` or `bubbletea`)
  - default: `simple`
- `--session-id`
  - resume an existing session ID

### `ssh`

- Same core provider flags as `tui`:
  - `-model` (default: `gpt-5.5` for openai, `z-ai/glm-5.1` for openrouter)
  - `-provider` LLM provider: `openai` or `openrouter` (default: `openai`)
  - `-timeout` request timeout (for example: `45s`, `2m`) (default: `20m`)
  - `-fs-root` filesystem sandbox root for file tools; if omitted, file tools are not registered (default: current working directory)
  - `-db-path` database path used for provider trace logging and per-session state (default: `db.sqlite`)
  - `-env-file` optional `.env` file path loaded before process environment values (default: `.env`)
  - `-bypass-compression-barrier` disable compression barrier middleware (default: `false`)
  - `--session-id` resume an existing session ID
- SSH-specific flag:
  - `-ssh-port` listen port for the SSH server (default: `23234`)
- Additional runtime behavior:
  - interface mode is fixed to `bubbletea` (the `--render` flag is not accepted)
  - prints `SSH server listening on port <port>` at startup
  - host key is persisted at `data/.ssh/host_ed25519`
  - public-key auth only (password and keyboard-interactive auth are disabled)
  - allowed SSH public keys come from `SSH_ALLOWED_PUBLIC_KEYS` (comma-separated authorized keys)

### `channel_listener`

- `--channel`
  - channel listener type (`telegram`)
- `-model`
  - default: `gpt-5.5` for `-provider openai`, `z-ai/glm-5.1` for `-provider openrouter`
- `-provider`
  - LLM provider: `openai` or `openrouter`
  - default: `openai`
- `-timeout` request timeout (for example: `45s`, `2m`)
  - default: `20m`
- `-fs-root`
  - reserved for shared config; filesystem tools are enabled only in interactive modes (`tui`, `ssh`)
  - default: current working directory
- `-db-path`
  - database path used for both provider trace logging and per-session provider state
  - default: `db.sqlite`
- `-env-file`
  - optional `.env` file path loaded before process environment values
  - default: `.env`
- `-bypass-compression-barrier`
  - disable compression barrier middleware
  - default: `false`
- `-telegram-poll-timeout`
  - `getUpdates` long-poll timeout in seconds
  - default: `30`

### `list_sessions`

- `-db-path`
  - database path used for both provider trace logging and per-session provider state
  - default: `db.sqlite`
- `-env-file`
  - optional `.env` file path loaded before process environment values
  - default: `.env`
- `-fs-root`
  - reserved for shared config; not used by `list_sessions`
  - default: current working directory

## Behavior notes

- The active backend is chosen with `-provider` (`openai` by default, or `openrouter`).
- Credentials are loaded in `main.go` during startup. The provider API key required for provider commands depends on `-provider`: `OPENAI_API_KEY` for `openai` and `OPENROUTER_API_KEY` for `openrouter`. `MATON_API_KEY` (optional) and `TELEGRAM_API_KEY` (required for `channel_listener --channel telegram`, optional otherwise to enable channel messaging tools) are loaded regardless of provider.
- The per-session cursor is stored as serialized JSON in the `previous_response` column: for `openai` it holds the Responses API response id, and for `openrouter` it holds the full conversation history so sessions resume across restarts. OpenAI and OpenRouter sessions are tracked separately because state is keyed by `(provider, session_id)`.
- The built-in OpenAI-hosted tools (`code_interpreter`, `web_search`) are not available with `-provider openrouter` because they cannot be called over the Chat Completions API; they are skipped, while local function tools (`get_time`, file tools, Maton, channel messaging) work unchanged.
- When `-env-file` is set (or default `.env` exists), values in that file are checked before process environment variables.
- Telegram allowlist is loaded from `TELEGRAM_ALLOWED_USERS` (comma-separated user IDs); empty means deny all.
- SSH allowlist is loaded from `SSH_ALLOWED_PUBLIC_KEYS` (comma-separated authorized public keys); required for `ssh` command.
- Tools always enabled in provider commands: `code_interpreter`, `web_search`, `get_time`.
- Interactive modes (`tui`, `ssh`) enable filesystem tools only when `-fs-root` is explicitly provided: `glob`, `grep`, `read`, `write`, `apply_patch`.
- File tool paths are virtualized when `-fs-root` is set: `/` maps to the configured sandbox root on disk, and all resolved paths are still validated against the allowed prefix policy.
- In sandbox mode, do not assume `/mnt/data` exists; discover available roots with `glob` using `path: "/"` and `pattern: "*"`.
- `write` creates parent directories automatically when the target path is inside missing directories within the sandbox.
- `send_channel_message` is enabled in interactive modes (`tui`, `ssh`) when `TELEGRAM_API_KEY` is set. It accepts `channel`, `user_id`, and `message`.
- `maton_gcalendar` and `maton_gmail` are enabled only when `MATON_API_KEY` is set.
- `maton_gcalendar` `list_events` requires `query.timeMin` and `query.timeMax` (RFC3339) to keep event queries bounded.
- When no TTY is detected for stdin/stdout, the app automatically uses
  simple line-based behavior.
- Storage errors are surfaced into the chat stream as `MsgTypeError` events
  while preserving normal response messages.
- Database persistence and trace logging are handled via Bun models and query builders.
- Telegram mode requires `TELEGRAM_API_KEY` for bot authentication.
- Telegram session history is isolated per user with session IDs in the format `telegram:<telegram_user_id>`.
