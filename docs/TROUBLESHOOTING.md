# Troubleshooting

Start with `homeserver.exe status`, `homeserver.exe diagnose` and **Admin → Diagnostics**. Logs: `homeserver.exe logs -n 200` (files in `%ProgramData%\HomeServer\logs`, rotated at 5 MB, four backups kept).

| Symptom | Likely cause | Fix |
|---|---|---|
| Phone cannot open the address | Firewall rule missing or network profile is Public | `homeserver.exe firewall add` (administrator); set the Wi-Fi network to **Private** |
| Works on the server but not from other devices | Router AP/client isolation, guest Wi-Fi, different subnet/VLAN | Connect both to the same normal Wi-Fi; disable client isolation |
| Address changed after reboot | DHCP gave a new IP | Reserve the laptop's address in the router; re-scan the QR code |
| Service will not start / "port is already in use" | Another program uses the port | `homeserver.exe set-port 8787`, `homeserver.exe restart`, `homeserver.exe firewall add` |
| Share shows **denied** | Service account (LocalService) cannot read the folder | `homeserver.exe grant "D:\Folder"` (administrator) or reinstall with `--account` |
| Share shows **offline** | Drive disconnected or folder renamed/deleted | Reconnect the drive; it recovers automatically. If the folder was renamed, remove and re-add the share |
| Upload fails with 507 | Server disk full | Free space on the drive holding the share (Diagnostics shows free space) |
| Upload fails with 413 | Over the configured upload limit | Admin → Settings → maximum upload size |
| "server is busy" (503) | All transfer slots in use | Wait, or raise *Concurrent transfers* in Settings |
| "too many failed attempts" | Login lockout | Wait up to 15 minutes, or restart the service |
| Forgot the admin password | — | On the server (administrator): `homeserver.exe set-password admin` |
| "database was corrupt" in the log | Disk error or manual damage | The bad file was kept as `homeserver.db.corrupt-<time>`; restore a backup with `homeserver.exe restore FILE` |
| Browser warns about the certificate | Self-signed HTTPS | Expected; accept once per device or supply your own certificate |
| Tray icon is grey | Service stopped | Right-click → Start service (UAC prompt) or `homeserver.exe start` |

## Checking the port by hand

```
netstat -ano | findstr :8686
Test-NetConnection -ComputerName <server-ip> -Port 8686      # run on another Windows PC
```

## Reinstall without losing data

Uninstall and answer **No** to deleting settings; reinstall. Shares and accounts return unchanged.
