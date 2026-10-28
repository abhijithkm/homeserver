# Architecture

## Components

| Component | Location | Role |
|---|---|---|
| Server + CLI | `cmd/homeserver` | HTTP server, Windows service host, admin CLI |
| Tray helper | `cmd/homeserver-tray` | Optional status icon; polls `runtime.json` + `/healthz`; service control via UAC. Never required by the server |
| HTTP API/UI | `internal/server` | Routing, auth middleware, handlers, static UI |
| Filesystem layer | `internal/fsx` | All access to shared folders |
| Store | `internal/store` | SQLite: settings, users, sessions, shares, audit, transfers |
| Auth | `internal/auth` | argon2id hashing, password/username policy, login throttling |
| Frontend | `web/` | React + TypeScript + Vite, built to `web/dist`, embedded with `go:embed` |

## Runtime layout (Windows)

- Binaries: `C:\Program Files\HomeServer\`
- Data: `%ProgramData%\HomeServer\` (`homeserver.db`, `logs\homeserver.log`, `tls\`, `runtime.json`). The installer restricts this folder to SYSTEM, Administrators and the service account.

## Request flow

1. Browser requests `/` -> static React bundle (immutable hashed assets).
2. `POST /api/login` -> argon2id verify -> random 256-bit session token in an `HttpOnly; SameSite=Strict` cookie (only its SHA-256 is stored) plus a per-session CSRF token.
3. Every `/api/shares/{id}/...` call: authenticate -> CSRF/Origin check for non-GET -> look up the share row -> check enabled/read-only/role -> `fsx.Open(path)` (re-validates that the folder exists *now*) -> operate -> close.

There is no cache of the filesystem, no directory index, no watcher and no polling timer in the server. Share availability is computed when asked, so a returning USB drive is served again immediately.

## Data model (SQLite, `PRAGMA user_version` migrations)

- `settings(key, value)`
- `users(id, username NOCASE unique, pw_hash, role admin|writer|reader, disabled, created_at)`
- `shares(id 16-hex, name unique, path, read_only default 1, enabled, created_at)`
- `sessions(token_hash, user_id, csrf, created_at, expires_at)`; expired rows are purged opportunistically on login
- `audit(ts, actor, ip, event, detail)` capped at 2,000 rows; `transfers(...)` capped at 500 rows

All statements are parameterised and multi-row changes are transactional. WAL mode; `quick_check` on open; corrupt files are moved aside; a database from a newer schema is refused and left untouched.

## API summary

Public: `GET /healthz`, `GET /api/setup-status`, `POST /api/setup` (loopback only, only while no user exists), `POST /api/login`.
Authenticated: `GET /api/session`, `POST /api/logout`, `POST /api/password`, `GET /api/network`, `GET /api/qr.png`, `GET /api/shares`, and per share: `GET list`, `GET download` (range), `POST upload` (raw body, `?path&name&conflict=fail|rename|replace`), `POST mkdir|rename|move|delete`.
Admin: shares CRUD, `GET/PUT /api/admin/settings`, users CRUD + password reset, `GET /api/admin/{status,diagnose,audit,transfers,logs,backup}`, `POST /api/admin/restart`.

## Transfers and memory

- Downloads: `http.ServeContent` over an open file (32 KiB copy buffers, range support). No file is ever read fully into memory.
- Uploads: raw request body copied with a pooled 64 KiB buffer into `<share>\.homeserver-tmp\u-<random>.part`, then atomically renamed into place under the chosen conflict policy (`O_EXCL` name reservation, so nothing is silently overwritten). Failure, cancel or client disconnect discards the partial file; stale partials are swept on startup and at most hourly per share on upload.
- Limits: configurable maximum upload size, concurrent transfer slots (waiting up to 2 s, then `503`), rolling 2-minute stall deadlines, header read timeout, 64 KiB JSON body cap.
- Directory listings stream `ReadDir` batches and keep only the requested window (offset+limit <= 10,000) in a bounded heap.
- Go runtime tuned with `GOGC=50` and a 256 MiB soft memory limit; argon2 memory (19 MiB) is returned to the OS after each hash.

## Service model

`homeserver.exe` detects when it is started by the Service Control Manager and runs under `golang.org/x/sys/windows/svc`. Stop/shutdown requests cancel the server context: listeners close, in-flight requests get 15 s to finish, the WAL is checkpointed and the database closed. A non-zero exit (startup failure) triggers the configured recovery actions (restart after 5 s / 30 s / 60 s). A named mutex prevents duplicate instances. An admin-requested restart from the UI re-reads settings and re-listens in-process.

## Deliberate non-goals

No mDNS, no WebDAV/SMB, no thumbnails/indexing, no resumable chunked uploads, no per-user per-share ACLs. These keep RAM and attack surface small.
