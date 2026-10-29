# Implementation and verification checklist

Legend: [x] implemented and verified by an automated test or a recorded manual run in this repository; [~] implemented but **not verified** in the environment used to build it; [ ] not implemented.

## Implementation
- [x] SQLite store, migrations, corruption recovery, backup/restore CLI (unit tests; restore command not exercised end-to-end)
- [x] Share registry: add/edit/enable/disable/remove, RO/RW, availability and automatic recovery (server tests)
- [x] Auth: argon2id, sessions, CSRF/Origin checks, rate limiting, roles, audit
- [x] Contained filesystem layer: traversal, NTFS junction containment, reserved names (fsx tests, run on Windows)
- [x] Streaming upload/download, HTTP range, conflict policy, temp cleanup, upload limit, interrupted upload
- [x] Admin API: settings, users, status, diagnose, logs, backup, restart
- [x] Optional HTTPS (self-signed verified by test; custom certificate path not tested)
- [x] React management UI (59 unit/component tests; manual check in a real browser: first-run setup, add share, browse; 375 px viewport has no horizontal overflow)
- [x] Installer, portable zip and build script produce artifacts; built server passes the smoke test
- [~] Windows service install/start/stop/recovery/uninstall (`svc_windows.go`): compiles and `status`/non-admin refusal were run, but the build session was not elevated, so installing the real service was **not** exercised
- [~] Installer behaviour (service registration, firewall rule, upgrade over an older version, uninstall prompts): compiled with Inno Setup, **not run**
- [x] Folder picker for Add share (`GET /api/admin/browse`, admin only): server tests (folders only, sorted, drive list, bad paths, role checks), component tests, and a manual run in a real browser at desktop and 375 px width. The manual run used a throwaway server running as the signed-in user, **not** as the `LocalService` account, so how many folders show "(no access)" under the real service was not checked
- [~] Installer adds its folder to the system PATH (default-on task, no duplicates, removed on uninstall): the script compiles, but the installer was **not run**, so the registry edit and its removal are unverified
- [~] Tray helper: starts and idles at ~18 MB; menu actions and UAC service control not exercised
- [~] File upload through a phone browser's picker / iOS Safari: not tested on physical devices
- [x] Update logic: version ordering, release selection, trusted-link check, checksum match/mismatch/missing, admin endpoint cache and opt-out (unit and server tests against a fake GitHub)
- [~] `homeserver update` end to end against real GitHub Releases, the silent installer upgrade it launches, and the tray "Check for updates" item: **not run** (needs a newer published release and an elevated session)
- [ ] mDNS discovery (not implemented)
- [ ] Resumable chunked uploads (not implemented)
- [ ] Per-user per-share ACLs (not implemented)

## Verification runs recorded
- `go vet ./...`, `gofmt -l` clean; `go test -count=1 ./...` passes.
- `go test -race ./internal/...` passes (race detector run with the WinLibs GCC toolchain; the release build itself is `CGO_ENABLED=0`).
- `npm run lint`, `npm test` (59 tests), `npm run build` pass.
- Second-client access: the server was reached through its LAN address (not loopback) from the same machine: login, listing, upload and share persistence across a process kill/restart verified. A different physical device was not used.
- Memory/performance: see [PERFORMANCE.md](PERFORMANCE.md).

## To verify on a Windows machine with administrator rights
1. Run the installer; confirm the service `HomeServer` exists, starts automatically after a reboot, and runs as `NT AUTHORITY\LocalService`.
2. `homeserver.exe grant` a folder, add it as a share, browse it from a phone.
3. Kill the service process (`taskkill /F /IM homeserver.exe`) and confirm Windows restarts it after ~5 s.
4. Install a newer build over an older one; confirm shares and accounts persist.
5. Uninstall; confirm the service and firewall rule are gone and data is kept or deleted according to the prompt.
