# Performance measurements

Measured with `scripts/perf.ps1` against the release-style build (`CGO_ENABLED=0`, `-trimpath`, stripped) on **2026-10-28**.

**Machine:** Windows 11 Home (10.0.26220), 12 logical CPUs, 23.9 GB RAM, local SSD. HomeServer, the load generator and the files were all on this one machine, over the loopback interface.

**Method**
- A fresh data folder and a free port; server started as a normal hidden process (not as a service); one admin account and one read/write share created through the API.
- Memory = Windows *working set* of `homeserver.exe` read through `Process.WorkingSet64` (the "Memory" column in Task Manager). *Private bytes* (committed memory) is also reported. During transfers the working set was **sampled** every 100 ms (the single 1 GB download was sampled every 64 MB), so a very short spike between samples would be missed.
- Idle: 10 s after start, then CPU time over a 30 s idle window.
- Large file: 1024 MB file created with `fsutil`; download read to memory-discarding sink with 1 MB reads; upload sent as a streamed body with `Content-Length`.
- Concurrency: 8 simultaneous downloads of 256 MB each.
- Large directory: 50,000 small files in one folder; listing requests for the first 200 entries.

**Results**

| Measurement | Result |
|---|---|
| Idle working set (10 s after start) | 17.2 MB (unchanged after 40 s) |
| Idle private bytes | 49.9 MB |
| Idle CPU (30 s window) | ~0 % |
| Working set during one 1 GB download | 18.7 MB peak (sampled) |
| Working set during one 1 GB upload | 18.8 MB peak |
| Working set, 8 concurrent 256 MB downloads | 19.7 MB peak; 19.7 MB 5 s after finishing |
| Goroutines after the concurrent test | 17 |
| Loopback throughput | download 458 MB/s (single), 927 MB/s (8 concurrent aggregate); upload 221 MB/s |
| List first 200 of 50,000 files | 117 ms by name, 109 ms by size (desc), 113 ms with a search filter; working set 19.6 -> 20.1 MB |

The idle target of under 30 MB working set is met on this machine. These are loopback numbers: they show the server and local disk are not the bottleneck, not what your Wi-Fi will deliver (typically tens of MB/s at best). They were measured once on one machine; treat them as indicative.

**Findings that drove optimisation**
- The first build idled at 81 MB because a package-level argon2 hash (19 MiB) ran at start-up. It is now computed lazily and memory is returned to the OS after every hash (`debug.FreeOSMemory`), giving the idle figure above.
- A concurrent-upload test exposed temp-file name collisions caused by the coarse Windows clock; names are now random.

An in-process test (`TestBoundedMemoryLargeTransfer`) additionally asserts that a 256 MiB upload plus download keeps Go heap growth under 64 MiB.

Reproduce: `powershell -File scripts\perf.ps1 -Exe dist\homeserver.exe`.
