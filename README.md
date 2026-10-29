# HomeServer

Lightweight, Windows-first file sharing for your home network. Register folders on one laptop once, and every phone, tablet and PC on the same Wi-Fi/LAN can browse, upload and download them from an ordinary web browser. No client app, no internet connection, no cloud.

- **Small and quiet**: a single Go executable (about 19 MB on disk) that runs as a Windows service; no Electron, no browser, no background scanning or indexing. Measured memory is in [docs/PERFORMANCE.md](docs/PERFORMANCE.md).
- **Persistent**: shares, accounts and settings live in SQLite and survive restarts, reboots, network drops and closing the management UI.
- **Safe by default**: nothing is shared until you register a folder; shares default to read-only; passwords are hashed with argon2id; paths cannot escape a share ([docs/SECURITY.md](docs/SECURITY.md)).
- **Phone friendly**: responsive web UI, QR code for the address, multi-file upload from the phone file picker.

## Architecture

```
 phone / laptop browser ──HTTP(S)──▶ homeserver.exe (Go, Windows service)
                                       ├─ embedded React UI (web/dist, go:embed)
                                       ├─ REST API + streaming file transfer
                                       ├─ os.Root-contained filesystem layer
                                       └─ SQLite (shares, users, sessions, audit, settings)
 homeserver-tray.exe  ── optional tray icon: status, open UI, start/stop (UAC)
```

The server is independent of the tray and of any open browser tab. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

Dependencies (Go): `modernc.org/sqlite` (pure Go, no C compiler), `golang.org/x/crypto` (argon2), `golang.org/x/sys` (Windows service), `golang.org/x/term`, `gopkg.in/natefinch/lumberjack.v2` (log rotation), `github.com/skip2/go-qrcode`, `github.com/getlantern/systray` (tray only). Frontend runtime: `react`, `react-dom`.

## Requirements

- Windows 10/11 x64 for the server (the server code itself also builds on Linux/macOS for development; service features are Windows-only).
- All devices on the same LAN/Wi-Fi. Guest Wi-Fi, client-isolated Wi-Fi and separate VLANs will not work unless your router routes between them.
- To build: Go (version in `go.mod`), Node.js + npm, Inno Setup 6 (installer only).

## Installation

1. Download `HomeServer-Setup-<version>.exe` from the [Releases](https://github.com/abhijithkm/homeserver/releases) page and run it (administrator approval is required to register the service).
2. Keep **Allow other devices on my home network** ticked: it adds a Windows Firewall rule scoped to **Private** networks only. Keep **Add HomeServer to the system PATH** ticked to run `homeserver` from any terminal (open a new terminal after installing; uninstalling removes the entry).
3. When it finishes, the service is installed (automatic start, delayed) and running.

Prefer the command line or the portable zip? Extract it somewhere permanent (for example `C:\Program Files\HomeServer`) and from an **administrator** terminal run:

```
homeserver.exe install
homeserver.exe firewall add
```

## First-run setup

1. On the server laptop open <http://localhost:8686>.
2. Create the administrator account (first-run setup works **only from the server computer itself**, so nobody on the LAN can claim an unconfigured server). Headless alternative: `homeserver set-password admin`.
3. Go to **Admin → Shares → Add share**, enter a name and the folder path (for example `D:\Photos`), keep **read-only** on unless people should be able to upload, and save.

### Folder permissions for the service account

By default the service runs as `NT AUTHORITY\LocalService` (least privilege). It can only read folders it has been granted access to. If a share shows *denied*, run in an administrator terminal:

```
homeserver.exe grant "D:\Photos"             :: read + write
homeserver.exe grant "D:\Photos" --readonly   :: read only
```

Alternatively install the service under your own account: `homeserver.exe install --account ".\YourName"` (you will be asked for your Windows password; it is passed to Windows and never stored by HomeServer).

## Adding and removing shares

Admin → Shares. You can rename, enable/disable, switch read-only/read-write and remove a share. **Removing a share never deletes files**; it only stops serving the folder. Folders on removable drives show *offline* while the drive is disconnected and come back automatically when it returns.

## Accessing HomeServer from a phone

1. Connect the phone to the same Wi-Fi as the server laptop.
2. On the server open **Admin → Dashboard** and scan the **QR code** with the phone camera, or type the address shown (for example `http://192.168.1.20:8686`).
3. Sign in, pick a share, then upload from the phone's file picker or download files. Tip: give the server laptop a DHCP reservation in your router so its address does not change.

Plain `http://` is not encrypted; see [HTTPS](#https-and-security-limits).

## Accounts and permissions

| Role | Can |
|---|---|
| admin | everything, including shares, users, settings, logs |
| writer | browse, download, upload, create/rename/move/delete (on read/write shares) |
| reader | browse and download |

A share set to **read-only** blocks all modification for every role, enforced on the server for every request.

## Windows service lifecycle

All from an administrator terminal in the install folder (`homeserver.exe <command>`):

| Command | Purpose |
|---|---|
| `install [--account A]` | install/upgrade and start the service (automatic delayed start, restart on failure) |
| `uninstall` | stop and remove the service (data and shared files are kept) |
| `start` / `stop` / `restart` | control the service |
| `status` | service state and whether the server answers |
| `logs [-n 200]` | last log lines (log folder is `%ProgramData%\HomeServer\logs`) |
| `diagnose` | checks the server on every local address and the firewall rule |
| `firewall add` / `firewall remove` | manage the Private-network firewall rule |
| `grant PATH [--readonly]` | give the service account access to a folder |
| `set-password USER`, `set-port N` | account recovery / change port |
| `backup FILE`, `restore FILE` | back up / restore configuration |
| `update [--check] [--yes] [--pre]` | find, verify and install a newer GitHub release |

Closing the tray icon, the browser or the management UI does not stop file sharing. The tray icon shows the server state: green tick (running), amber dots (starting or stopping), red square (stopped), orange `!` (service running but not answering), grey dash (not installed). Hover for the text.

## Firewall

The installer adds an inbound rule for `homeserver.exe` on **Private** networks only. Manual equivalent:

```
netsh advfirewall firewall add rule name="HomeServer" dir=in action=allow program="C:\Program Files\HomeServer\homeserver.exe" enable=yes profile=private protocol=TCP
```

Do not disable the firewall, and do not port-forward HomeServer on your router: it is designed for the local network only. Make sure the Wi-Fi network is marked **Private** in Windows settings.

## HTTPS and security limits

By default HomeServer speaks plain HTTP, which is common for a trusted home LAN but means anyone who can observe your Wi-Fi traffic can read passwords and files. For stronger protection use **Admin → Settings → HTTPS → self-signed** (browsers will show a one-time certificate warning because the certificate is not issued by a public authority) or supply your own certificate and key. Full threat model: [docs/SECURITY.md](docs/SECURITY.md).

## Troubleshooting

See [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md). Quick checks: `homeserver.exe status`, `homeserver.exe diagnose`, **Admin → Diagnostics**.

## Backup and restore

- Web: **Admin → Diagnostics → Download backup**, or `homeserver.exe backup C:\safe\homeserver.db`.
- Restore: `homeserver.exe stop`, `homeserver.exe restore C:\safe\homeserver.db`, `homeserver.exe start`. The previous database is kept alongside as `homeserver.db.before-restore-<time>`.
- The backup contains password hashes: store it privately. Your shared files are not part of it.
- A corrupt database is moved aside (`homeserver.db.corrupt-<time>`) and the server starts with an empty configuration; restore a backup to recover.

## Updating

**From GitHub releases.** The Admin dashboard shows a notice when a newer release exists (Settings → Software updates controls this; pre-releases are opt-in). To install it, run `homeserver.exe update` in an administrator terminal (add `--check` to only look, `--pre` for pre-releases), or use the tray icon → **Check for updates...** (UAC prompt). The installer is downloaded from the release, its SHA-256 is verified against the release's `SHA256SUMS` file, and it then runs silently. Nothing is ever installed automatically, and the service itself never downloads or runs code. Limits: the checksum only detects corrupt downloads, and installers are not code-signed ([docs/SECURITY.md](docs/SECURITY.md#updates)).

**Manually.** Run the new installer over the old one. It stops the service, replaces the files, re-applies the service configuration and starts it again; settings, accounts and shares are preserved (the database schema migrates automatically; a database from a newer version is never touched by an older one).

## Uninstalling

Use **Apps → Installed apps → HomeServer → Uninstall**. The service and firewall rule are removed; you are asked whether to also delete the configuration in `%ProgramData%\HomeServer`. Shared files are never deleted.

## Build instructions

```
winget install GoLang.Go OpenJS.NodeJS JRSoftware.InnoSetup
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Version 1.0.0
```

The script verifies tools, builds and tests the frontend, formats/vets/tests the Go code, builds `homeserver.exe` and `homeserver-tray.exe` (`CGO_ENABLED=0`, `-trimpath`), smoke-tests the built server, and produces `release\HomeServer-Setup-<v>.exe`, `release\HomeServer-<v>-windows-amd64.zip` and `release\SHA256SUMS.txt`. Details: [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

## Testing

```
go test ./...             # backend (add -race if a C compiler is installed)
cd web && npm test        # frontend
```

## Known limitations

- Windows 10/11 x64 only for the packaged release. Real Windows-service installation, the installer wizard and upgrade-over-old-version have **not** been exercised on a machine yet (see [docs/CHECKLIST.md](docs/CHECKLIST.md) for exactly what was and was not verified).
- HTTP by default (see above). No mDNS/`.local` name yet: use the IP address (reserve it in your router) or your own DNS name.
- Symlinks, junctions and other reparse points inside a share are deliberately hidden and refused rather than followed.
- Uploads are single streamed requests, not resumable chunks: an interrupted upload must be restarted (partial files are discarded). Downloads support HTTP range requests.
- Roles are global (admin/writer/reader); there are no per-user per-share grants yet.
- A directory listing window is limited to 10,000 items (use search/sort to narrow).
- UNC/network-drive shares depend on the service account's network credentials and are not tested.
- iOS Safari: uploads/downloads use standard browser features and were not tested on a physical iPhone.

## License

[MIT](LICENSE)
