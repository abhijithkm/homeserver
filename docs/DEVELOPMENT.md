# Development guide

## Layout

```
cmd/homeserver        server + CLI (service code in svc_windows.go, stubs in svc_other.go)
cmd/homeserver-tray   tray helper
internal/{server,fsx,store,auth,netinfo,paths,tlsutil}
web/                  React + TypeScript + Vite app; web/embed.go embeds web/dist
installer/            Inno Setup script
scripts/              build.ps1 (release), smoke.ps1 (post-build check)
docs/
```

## Prerequisites

Go (version in `go.mod`), Node.js 20+ with npm, Inno Setup 6 for the installer. No C compiler is required: the SQLite driver is `modernc.org/sqlite` (pure Go) and release builds use `CGO_ENABLED=0`. The race detector (`go test -race`) needs CGO and a C compiler (for example WinLibs/MinGW-w64); `scripts/build.ps1` uses it automatically when `gcc` is on `PATH`.

## Run locally

```
cd web && npm ci && npm run build && cd ..
go run ./cmd/homeserver --data .\.devdata run        # http://localhost:8686
cd web && npm run dev                                 # Vite dev server on :5173, proxies /api to :8686
```

`--data` (or `HOMESERVER_DATA`) selects the data folder; without it the Windows default is `%ProgramData%\HomeServer`.

## Tests

```
go vet ./... && go test -count=1 ./...       # add -race when gcc is available
cd web && npm run lint && npm test
```

Go tests cover containment (including a real NTFS junction), conflict policies, interrupted uploads, concurrent transfers, sessions, CSRF, rate limiting, migrations and corruption recovery. `TestBoundedMemoryLargeTransfer` pushes 256 MiB up and down and asserts bounded heap growth (skipped with `-short`).

## Conventions

- Standard library first; every dependency must be justified in the README.
- All client-supplied paths go through `fsx`; never `filepath.Join` a client string onto a share path elsewhere.
- Never log passwords, tokens or file contents; never return server paths to non-admins.
- No background pollers or caches without a documented lifecycle.
- Commit author for this repository is the owner's personal address (set with `git config --local`).

## Release

`scripts\build.ps1 -Version X.Y.Z` -> artifacts in `release\`. Tag `vX.Y.Z`, push, then `gh release create vX.Y.Z release\* --title ... --notes-file ...` only after the checklist in `docs/CHECKLIST.md` passes.
