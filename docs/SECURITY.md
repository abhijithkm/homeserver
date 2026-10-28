# Security model

## Assumptions and scope

HomeServer protects files on a **trusted home LAN** from: other devices on the LAN without an account, malicious web pages visited by a logged-in user, malformed requests (traversal, symlink tricks) and accidental modification. It does **not** protect against: an attacker who can read your Wi-Fi traffic when using plain HTTP, a compromised server laptop, a malicious administrator, or an attacker with local administrator rights on the server.

## Controls

| Threat | Control |
|---|---|
| Unauthenticated access | Every API except `/healthz`, `/api/setup-status`, `/api/setup`, `/api/login` requires a session; no unauthenticated admin endpoint |
| Claiming a fresh server | First-run setup is accepted only from loopback and only while there are zero users |
| Password theft from the database | argon2id (19 MiB, t=2, p=1), per-password random salt, constant-time compare, 8-256 character policy |
| Session theft/forgery | 256-bit random token; only its SHA-256 stored; `HttpOnly`, `SameSite=Strict`, `Secure` under HTTPS; expiry (default 24 h, configurable); logout and password change revoke sessions |
| CSRF | `SameSite=Strict` cookie **and** required `X-CSRF-Token` on every non-GET **and** `Origin` must equal `Host` |
| Brute force | 5 failures per IP and per username in 15 min -> exponential lockout up to 15 min (HTTP 429); dummy hashing hides which usernames exist |
| Path traversal | Client paths are split into segments; `.`, `..`, empty, `\ / : * ? " < > |`, control characters, trailing dot/space, Windows device names and the internal temp dir name are rejected; all I/O goes through `os.Root`, which refuses to leave the share root |
| Symlink / junction / reparse-point escape | Every path component is `Lstat`-ed and any symlink, junction or other reparse point is refused; such entries are hidden from listings; `os.Root` is a second layer. Verified by automated tests on Windows using a junction |
| NTFS alternate data streams | `:` is rejected in names |
| Overwriting/deleting | Writes require share read/write **and** role admin/writer, re-checked on every request; uploads never overwrite unless `conflict=replace`; rename/move never overwrite; the UI confirms deletes |
| Exposing the filesystem | Only folders explicitly registered by an admin are reachable; clients only ever see share IDs and relative paths; filesystem paths are shown to admins only; the server data folder cannot be shared |
| XSS via shared files | Downloads are `attachment` by default; inline only for image/video/audio/PDF/plain-text with `nosniff` and a `sandbox` CSP; SVG/HTML are never rendered inline |
| Clickjacking / MIME sniffing | `X-Frame-Options: DENY`, CSP `frame-ancestors 'none'`, `nosniff` |
| CORS | No CORS headers are ever sent; `OPTIONS` is refused |
| SQL injection | Parameterised statements only; share IDs validated as 16 hex characters |
| Resource exhaustion | Request/JSON limits, upload cap, transfer slots, header and stall timeouts, bounded listing windows, bounded rate-limiter table |
| Secret leakage | Passwords, tokens and file contents are never logged; error responses never include server paths |
| Privilege | Service runs as `LocalService` by default; data folder ACL restricted; only install/firewall/grant need administrator |

## Network exposure

- Bind address defaults to `0.0.0.0` so LAN devices can connect; set `bindAddr` to `127.0.0.1` for local-only use.
- The installer's firewall rule applies to **Private** profiles only. Never port-forward HomeServer.
- Plain HTTP: credentials and files are readable by anyone who can sniff the network. Enable HTTPS in Settings (self-signed: a one-time browser warning is expected, and the warning also means you cannot distinguish a man-in-the-middle from your own server until you verify the certificate; custom certificate: provide PEM files readable by the service account).

## Known limitations

- The login rate limiter is in-memory per process (resets on restart).
- Roles are global; any signed-in user can see every enabled share.
- Time-of-check/time-of-use between the reparse-point check and the open is narrowed by `os.Root` but a local attacker with write access inside a share could still race ordinary rename operations; do not share folders that untrusted local users can modify concurrently.
- Security-relevant events are in the audit table and log; there is no alerting.
- The database holds password hashes; protect backups.

Report vulnerabilities privately to the repository owner via GitHub security advisories.
