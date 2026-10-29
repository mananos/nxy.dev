# nxy 2.0 dev plugin

Dev only. Same hook events as the 1.x plugin, but each one runs the Go binary through the shim `bin/nxy.cmd`. In F0 the binary answers nothing.

Try it:

```
go build -o <dir>/nxy.exe ./cmd/nxy        # no .exe suffix on Linux/macOS
NXY_BIN=<dir>/nxy.exe claude --plugin-dir dev/plugin
```

What you should see: nothing. No hook output, no blocked tool, no extra text in the session. A stderr line starting with `nxy:` means the shim could not find or fetch the binary.

It sits beside the installed 1.x plugin (`nxy`, version 1.x): both load, both hooks run on every event. The 1.x plugin does the real work; this one only proves the binary starts. Its name is `nxy2-dev`, so it does not replace the installed one.

Download limits: the shim fetches at most 8 s in total (binary 6 s, checksums 2 s), under the 10 s hook timeout. Only one hook downloads at a time (lock folder `nxy.exe.lock` beside the target); the others print one `nxy:` line and do nothing. A lock older than a minute and temp files older than a day, left by dead runs, are removed on the next start.

Known limit: the SHA-256 in `checksums.txt` comes from the same GitHub Release as the binary. It protects against corruption and truncation, not against a compromised release. A signature checked against a pinned key (cosign or minisign) is a later step.

`NXY_BIN` wins over everything. To test the download path (binary under `~/.nxy/bin/2.0.0-dev/`, fetched from the GitHub release), unset it first: a leftover `NXY_BIN` silently runs a stale build.
