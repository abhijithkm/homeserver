# Changelog

All notable changes to HomeServer are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Update from GitHub releases: `homeserver update` (checksum-verified download, UAC, silent installer run), a tray menu item, and a dashboard notice with Settings to turn the check off or include pre-releases. Nothing installs automatically.
- Tray icon now has a distinct icon per state: running, starting/stopping, stopped, not answering, not installed.

- Add share has a **Browse...** folder picker (admin only) that lists the server's drives and folders, so the path no longer has to be typed. Hidden/system folders, links and junctions are not listed; folders the service account cannot read are shown as "(no access)".
- Installer task (on by default) adds the install folder to the system PATH; the entry is removed on uninstall and never duplicated on upgrade.

### Fixed
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
