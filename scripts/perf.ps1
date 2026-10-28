<#
  Measures homeserver.exe memory, CPU, throughput and large-directory listing on this machine.
  Usage: scripts\perf.ps1 -Exe dist\homeserver.exe [-BigMB 1024] [-ManyFiles 50000]
  Everything runs on loopback against a throwaway data folder; nothing is installed.
  Note: loopback throughput measures the server and disk, not Wi-Fi speed.
#>
param(
    [Parameter(Mandatory)][string]$Exe,
    [int]$BigMB = 1024,
    [int]$ConcurrentMB = 256,
    [int]$Concurrency = 8,
    [int]$ManyFiles = 50000
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
$Exe = (Resolve-Path $Exe).Path
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("hs-perf-" + [guid]::NewGuid().ToString('N'))
$data = Join-Path $tmp 'data'; $share = Join-Path $tmp 'share'
New-Item -ItemType Directory -Force $data, $share | Out-Null
$l = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0); $l.Start(); $port = $l.LocalEndpoint.Port; $l.Stop()
& $Exe --data $data set-port $port | Out-Null
$env:HOMESERVER_PASSWORD_STDIN = '1'
'perf-pass-123' | & $Exe --data $data set-password perf | Out-Null
$srv = Start-Process -FilePath $Exe -ArgumentList '--data', $data, 'run' -PassThru -WindowStyle Hidden
$base = "http://127.0.0.1:$port"
function Mb($b) { [math]::Round($b / 1MB, 1) }
function Snap { $srv.Refresh(); [pscustomobject]@{ WS = $srv.WorkingSet64; Priv = $srv.PrivateMemorySize64; Peak = $srv.PeakWorkingSet64 } }
try {
    for ($i = 0; $i -lt 40; $i++) { Start-Sleep -Milliseconds 250; try { if ((Invoke-RestMethod "$base/healthz").status -eq 'ok') { break } } catch { } }
    $web = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $r = Invoke-RestMethod "$base/api/login" -Method Post -ContentType 'application/json' -Body (@{ username = 'perf'; password = 'perf-pass-123' } | ConvertTo-Json) -WebSession $web
    $h = @{ 'X-CSRF-Token' = $r.csrf }
    $sh = Invoke-RestMethod "$base/api/shares" -Method Post -ContentType 'application/json' -Headers $h -WebSession $web -Body (@{ name = 'perf'; path = $share; readOnly = $false } | ConvertTo-Json)
    $id = $sh.id

    # ---- idle ----
    Start-Sleep -Seconds 10
    $idle = Snap
    $cpu0 = $srv.TotalProcessorTime; $t0 = Get-Date
    Start-Sleep -Seconds 30
    $srv.Refresh(); $cpuPct = (($srv.TotalProcessorTime - $cpu0).TotalSeconds / ((Get-Date) - $t0).TotalSeconds / [Environment]::ProcessorCount) * 100
    $idle2 = Snap

    # HttpClient with the session cookie
    $handler = [Net.Http.HttpClientHandler]::new(); $handler.CookieContainer = [Net.CookieContainer]::new()
    foreach ($c in $web.Cookies.GetCookies([uri]$base)) { $handler.CookieContainer.Add([uri]$base, $c) }
    $http = [Net.Http.HttpClient]::new($handler); $http.Timeout = [TimeSpan]::FromMinutes(30)
    $http.DefaultRequestHeaders.Add('X-CSRF-Token', $r.csrf)

    # ---- large file ----
    & fsutil file createnew (Join-Path $share 'big.bin') ([int64]$BigMB * 1MB) | Out-Null
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $dlTask = $http.GetAsync("$base/api/shares/$id/download?path=big.bin", [Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
    $stream = $dlTask.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    $buf = New-Object byte[] (1MB); $peakDlWS = 0; $total = 0L
    while (($n = $stream.Read($buf, 0, $buf.Length)) -gt 0) { $total += $n; if (($total % (64MB)) -lt 1MB) { $s = Snap; if ($s.WS -gt $peakDlWS) { $peakDlWS = $s.WS } } }
    $stream.Dispose(); $sw.Stop()
    $dlMBps = [math]::Round($total / 1MB / $sw.Elapsed.TotalSeconds, 0)

    $sw.Restart(); $peakUpWS = 0
    $fs = [IO.File]::OpenRead((Join-Path $share 'big.bin'))
    $content = [Net.Http.StreamContent]::new($fs, 1MB); $content.Headers.ContentLength = $fs.Length
    $task = $http.PostAsync("$base/api/shares/$id/upload?name=big-up.bin&conflict=replace", $content)
    while (-not $task.IsCompleted) { $s = Snap; if ($s.WS -gt $peakUpWS) { $peakUpWS = $s.WS }; Start-Sleep -Milliseconds 100 }
    $resp = $task.GetAwaiter().GetResult(); $sw.Stop(); $fs.Dispose()
    if ([int]$resp.StatusCode -ne 201) { throw "upload failed: $($resp.StatusCode)" }
    $upMBps = [math]::Round($BigMB / $sw.Elapsed.TotalSeconds, 0)

    # ---- concurrent downloads ----
    $names = 1..$Concurrency | ForEach-Object { $n = "c$_.bin"; & fsutil file createnew (Join-Path $share $n) ([int64]$ConcurrentMB * 1MB) | Out-Null; $n }
    $sw.Restart()
    $tasks = $names | ForEach-Object {
        $resp = $http.GetAsync("$base/api/shares/$id/download?path=$_", [Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
        $resp.Content.CopyToAsync([IO.Stream]::Null)
    }
    $peakCWS = 0
    while ($tasks | Where-Object { -not $_.IsCompleted }) { $s = Snap; if ($s.WS -gt $peakCWS) { $peakCWS = $s.WS }; Start-Sleep -Milliseconds 100 }
    $sw.Stop(); $concMBps = [math]::Round($Concurrency * $ConcurrentMB / $sw.Elapsed.TotalSeconds, 0)
    $after = Snap
    Start-Sleep -Seconds 5; [void](Invoke-RestMethod "$base/api/admin/status" -WebSession $web); $settled = Snap
    $status = Invoke-RestMethod "$base/api/admin/status" -WebSession $web

    # ---- many files ----
    $many = Join-Path $share 'many'; New-Item -ItemType Directory $many | Out-Null
    $sw.Restart(); 1..$ManyFiles | ForEach-Object { [IO.File]::WriteAllBytes((Join-Path $many ("file-{0:D6}.txt" -f $_)), [byte[]]@(65)) }; $sw.Stop()
    $createSec = [math]::Round($sw.Elapsed.TotalSeconds, 0)
    $before = Snap
    $sw.Restart(); $first = Invoke-RestMethod "$base/api/shares/$id/list?path=many&limit=200&sort=name" -WebSession $web; $sw.Stop(); $listName = $sw.ElapsedMilliseconds
    $sw.Restart(); $bySize = Invoke-RestMethod "$base/api/shares/$id/list?path=many&limit=200&sort=size&desc=1" -WebSession $web; $sw.Stop(); $listSize = $sw.ElapsedMilliseconds
    $sw.Restart(); $q = Invoke-RestMethod "$base/api/shares/$id/list?path=many&q=file-04999&limit=50" -WebSession $web; $sw.Stop(); $listQ = $sw.ElapsedMilliseconds
    $afterList = Snap

    "## Results ($(Get-Date -Format 'yyyy-MM-dd'))"
    "- Machine: $([Environment]::OSVersion.VersionString), $([Environment]::ProcessorCount) logical CPUs, $([math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB, 1)) GB RAM; build $($status.version) ($($status.os)); loopback, throwaway data folder."
    "- Idle (10 s after start): working set $(Mb $idle.WS) MB, private bytes $(Mb $idle.Priv) MB; after 40 s idle: $(Mb $idle2.WS) MB. CPU while idle (30 s): $([math]::Round($cpuPct,2)) % of total CPU."
    "- Download of one $BigMB MB file: peak working set (sampled) $(Mb $peakDlWS) MB, $dlMBps MB/s."
    "- Upload of one $BigMB MB file: peak working set $(Mb $peakUpWS) MB, $upMBps MB/s."
    "- $Concurrency concurrent downloads x $ConcurrentMB MB: peak working set $(Mb $peakCWS) MB, aggregate $concMBps MB/s; after: $(Mb $after.WS) MB; 5 s later: $(Mb $settled.WS) MB; goroutines now $($status.goroutines)."
    "- Directory with $ManyFiles files (created in ${createSec}s): list first 200 by name $listName ms (total $($first.total)), 200 by size desc $listSize ms, search in folder $listQ ms (matches $($q.total)); working set before/after listing $(Mb $before.WS) / $(Mb $afterList.WS) MB."
} finally {
    if ($srv -and -not $srv.HasExited) { Stop-Process -Id $srv.Id -Force }
    Start-Sleep -Milliseconds 500
    Get-ChildItem $tmp -Recurse -Force -ErrorAction SilentlyContinue | Sort-Object FullName -Descending | ForEach-Object { try { $_.Delete() } catch { } }
}
