<#
  Starts a built homeserver.exe against a throwaway data folder on a free port and
  verifies it serves /healthz, completes first-run setup and serves the web UI.
  Usage: scripts\smoke.ps1 -Exe dist\homeserver.exe
#>
param([Parameter(Mandatory)][string]$Exe)
$ErrorActionPreference = 'Stop'
$Exe = (Resolve-Path $Exe).Path
$data = Join-Path ([IO.Path]::GetTempPath()) ("hs-smoke-" + [guid]::NewGuid().ToString('N'))
$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0); $listener.Start()
$port = $listener.LocalEndpoint.Port; $listener.Stop()
& $Exe --data $data set-port $port | Out-Null
$p = Start-Process -FilePath $Exe -ArgumentList '--data', $data, 'run' -PassThru -WindowStyle Hidden
try {
    $base = "http://127.0.0.1:$port"
    $ok = $false
    for ($i = 0; $i -lt 40 -and -not $ok; $i++) {
        Start-Sleep -Milliseconds 250
        try { $ok = (Invoke-RestMethod "$base/healthz").status -eq 'ok' } catch { }
    }
    if (-not $ok) { throw "server did not become healthy on port $port" }
    $s = Invoke-RestMethod "$base/api/setup-status"
    if (-not $s.needsSetup) { throw 'fresh data folder should need setup' }
    $html = Invoke-WebRequest "$base/" -UseBasicParsing
    if ($html.StatusCode -ne 200) { throw 'UI not served' }
    if (-not $html.Headers['Content-Security-Policy']) { throw 'security headers missing' }
    $body = @{ username = 'smoke'; password = 'smoke-test-pass' } | ConvertTo-Json
    $web = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $r = Invoke-RestMethod "$base/api/setup" -Method Post -Body $body -ContentType 'application/json' -WebSession $web
    if (-not $r.csrf) { throw 'setup did not return a session' }
    $null = Invoke-RestMethod "$base/api/session" -WebSession $web
    Write-Host "SMOKE OK: $Exe served healthz, UI, setup and session on port $port"
} finally {
    if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force }
    Start-Sleep -Milliseconds 300
    Get-ChildItem $data -Recurse -Force -ErrorAction SilentlyContinue | ForEach-Object { try { $_.Delete() } catch { } }
}
