# Changelog

All notable changes to HomeServer are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Update from GitHub releases: `homeserver update` (checksum-verified download, UAC, silent installer run), a tray menu item, and a dashboard notice with Settings to turn the check off or include pre-releases. Nothing installs automatically.
- Tray icon now has a distinct icon per state: running, starting/stopping, stopped, not answering, not installed.

- Add share has a **Browse...** folder picker (admin only) that lists the server's drives and folders, so the path no longer has to be typed. Hidden/system folders, links and junctions are not listed; folders the service account cannot read are shown as "(no access)".
- Tray menu **Grant folder access...**: choose a folder in the Windows dialog, pick read-only or read/write, and HomeServer's service account is granted access. Folders you own need no administrator prompt; otherwise Windows asks for approval (UAC). `homeserver.exe grant` itself no longer requires an elevated terminal when you own the folder, refuses whole drives, and has a `--pause` flag.
- **Copy command** button for the exact `grant` command in the Add share error and for "(no access)" folders in the folder picker.
- Installer task (on by default) adds the install folder to the system PATH; the entry is removed on uninstall and never duplicated on upgrade.

### Changed
- New web interface for administrators and everyone else: a dark side rail on wide screens that becomes a top bar with a bottom tab bar on tablets and phones, shares shown as drive bays with a status light, stacked file rows on phones, bottom-sheet dialogs, and the same teal and ink look in light and dark mode. Checked at 320 px with no horizontal scrolling.
- Install as an app: HomeServer now has a web app manifest, icons and a small service worker (it caches nothing), and the Account page has an **Install on this device** card. iPhone/iPad can Add to Home Screen over HTTP; Android and desktop Chrome/Edge offer a full install only over HTTPS with a certificate the device trusts, and otherwise add a shortcut. See the README.
- Navigation: administrators now land on the Dashboard when they open HomeServer (everyone else lands on Files). The side rail lists Dashboard and Files, then a Manage group (Shares, Users, Settings, Diagnostics), then Account; on phones the bottom bar has Dashboard, Files, Manage and Account, and Manage opens the section tabs. The page title names the current section, and the HomeServer logo links home.
- Admin dashboard is now a full status page: a health headline with lamps for server, database, network, shares, disk space, HTTPS and updates; live tiles for uptime, transfer slots, memory, shares and accounts; disk meters per share; the connect address and QR code; recent transfers with a summary; recent events; and server details with restart and backup. It refreshes by itself every 10 seconds while the tab is open.

### Fixed
- Adding a share the service account cannot read (for example a folder under `C:\Users\<name>`) now says so and shows the `homeserver.exe grant` command, instead of the raw "Access is denied" system error.
- Tray status no longer depends on reading `runtime.json` from the administrator-only data folder; it asks the Windows service manager for the state and probes the default port.

## [1.0.0] - Unreleased

### Added
- Go server with SQLite (pure-Go driver) configuration store, schema migrations and corruption recovery.
- Persistent share registry: add, rename, enable/disable, read-only or read/write, remove (never deletes files), automatic offline/online detection.
- Authenticated browser file manager: list/sort/search/paginate, download (HTTP range), streamed multi-file upload with conflict policy, mkdir, rename, move, delete.
- Accounts (admin / writer / reader), argon2id password hashing, server-side sessions, CSRF protection, login rate limiting, audit log.
- Containment: `os.Root`-based path resolution plus rejection of symlinks, junctions and reparse points, name validation (reserved names, alternate data streams).
- Optional HTTPS (self-signed or custom certificate).
- Windows service integration (`install`, `uninstall`, `start`, `stop`, `restart`, `status`, `logs`, `diagnose`, `firewall`, `grant`), recovery actions, single-instance guard.
- Tray helper, Inno Setup installer, portable zip, reproducible build scripts.
- React/TypeScript management UI with QR code, dashboard, diagnostics, backups.
