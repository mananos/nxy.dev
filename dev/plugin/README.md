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

## F1: the filter

PreToolUse on Bash/PowerShell now rewrites the command through rtk exactly like the 1.x plugin (same golden-tested rules). Disable the installed 1.x plugin while you load `dev/plugin`: both would rewrite the same command. Everything else still answers nothing.

Startup time: `nxy bench hook [-n 20] [-command "git status"] [-shim bin/nxy.cmd] [-bin <nxy>]` runs the real hook (filter off, filter on, and through the shim) and prints min/median/p90/max in ms against the 50 ms threshold. It always exits 0; CI prints the table in the job summary on Linux and Windows.

Golden files: regenerate with `node scripts/golden/filter.mjs` (add `--record` for new commands, needs rtk; the hook area needs `go`). `--check` only compares.

`NXY_BIN` wins over everything. To test the download path (binary under `~/.nxy/bin/2.0.0-dev/`, fetched from the GitHub release), unset it first: a leftover `NXY_BIN` silently runs a stale build.
