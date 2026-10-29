# Troubleshooting

Start with `homeserver.exe status`, `homeserver.exe diagnose` and **Admin → Diagnostics**. Logs: `homeserver.exe logs -n 200` (files in `%ProgramData%\HomeServer\logs`, rotated at 5 MB, four backups kept).

| Symptom | Likely cause | Fix |
|---|---|---|
| Phone cannot open the address | Firewall rule missing or network profile is Public | `homeserver.exe firewall add` (administrator); set the Wi-Fi network to **Private** |
| Android phone cannot open the address (or even the router page, e.g. `192.168.1.1`); Chrome shows `ERR_TOO_MANY_RETRIES` or "This site can't be reached" while the laptop and other devices work | Android blocks the browser from reaching devices on your Wi-Fi until it has the local-network permission | Settings → Apps → your browser → Permissions → **Nearby devices** (or **Local network**) → **Allow**, then close and reopen the browser |
| Works on the server but not from other devices | Router AP/client isolation, guest Wi-Fi, different subnet/VLAN | Connect both to the same normal Wi-Fi; disable client isolation |
| Address changed after reboot | DHCP gave a new IP | Reserve the laptop's address in the router; re-scan the QR code |
| Service will not start / "port is already in use" | Another program uses the port | `homeserver.exe set-port 8787`, `homeserver.exe restart`, `homeserver.exe firewall add` |
| Adding a share fails with "cannot read this folder" | The service account (LocalService) has no access to it, typical for folders under `C:\Users\<you>` | Tray icon → **Grant folder access...** (no administrator prompt for your own folders), or paste the command from the **Copy command** button: `homeserver.exe grant "C:\Users\you\Downloads"` (add `--readonly` for a read-only share), then add the share again |
| Share shows **denied** | Service account (LocalService) cannot read the folder | `homeserver.exe grant "D:\Folder"` (administrator) or reinstall with `--account` |
| Share shows **offline** | Drive disconnected or folder renamed/deleted | Reconnect the drive; it recovers automatically. If the folder was renamed, remove and re-add the share |
| Upload fails with 507 | Server disk full | Free space on the drive holding the share (Diagnostics shows free space) |
| Upload fails with 413 | Over the configured upload limit | Admin → Settings → maximum upload size |
| "server is busy" (503) | All transfer slots in use | Wait, or raise *Concurrent transfers* in Settings |
| "too many failed attempts" | Login lockout | Wait up to 15 minutes, or restart the service |
| Forgot the admin password | — | On the server (administrator): `homeserver.exe set-password admin` |
| "database was corrupt" in the log | Disk error or manual damage | The bad file was kept as `homeserver.db.corrupt-<time>`; restore a backup with `homeserver.exe restore FILE` |
| Browser warns about the certificate | Self-signed HTTPS | Expected; accept once per device or supply your own certificate |
| Tray icon has a red square | Service stopped | Right-click → Start service (UAC prompt) or `homeserver.exe start` |
| Tray icon has an orange `!` | Service is running but `/healthz` does not answer (a custom port is probed only if the tray can read `runtime.json`; otherwise 8686) | `homeserver.exe diagnose`, `homeserver.exe logs` |
| Tray icon has a grey dash | Service not installed | Run the installer, or `homeserver.exe install` as administrator |

## Checking the port by hand

```
netstat -ano | findstr :8686
Test-NetConnection -ComputerName <server-ip> -Port 8686      # run on another Windows PC
```

## Reinstall without losing data

Uninstall and answer **No** to deleting settings; reinstall. Shares and accounts return unchanged.
