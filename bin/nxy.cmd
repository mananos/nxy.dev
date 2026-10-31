: <<'BATCH'
@echo off
rem nxy shim, batch half (PowerShell / cmd on Windows). No labels, no goto: cmd mis-parses them in LF files.
rem Delayed expansion stays off until a failure echo (a ! in a path would be eaten).
setlocal DisableDelayedExpansion
set "NXY_VERSION=2.0.0-dev"
set "NXY_FAIL="
set "NXY_NEED="
set "NXY_GOT="
set "NXY_WANT="
if defined NXY_HOME (set "NXY_DIR=%NXY_HOME%") else (set "NXY_DIR=%USERPROFILE%\.nxy")
set "NXY_EXE=%NXY_DIR%\bin\%NXY_VERSION%\nxy.exe"
if defined NXY_BIN (set "NXY_RUN=%NXY_BIN%") else (set "NXY_RUN=%NXY_EXE%")
set "NXY_ARCH=amd64"
if /i "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "NXY_ARCH=arm64"
set "NXY_ASSET=nxy_%NXY_VERSION%_windows_%NXY_ARCH%.exe"
if defined NXY_RELEASE_URL (set "NXY_URL=%NXY_RELEASE_URL%") else (set "NXY_URL=https://github.com/mananos/nxy.dev/releases/download/v%NXY_VERSION%")
set "NXY_TMP=%NXY_EXE%.%RANDOM%%RANDOM%.tmp"
set "NXY_SUMS=%NXY_EXE%.%RANDOM%%RANDOM%.sums"
if not defined NXY_BIN if not exist "%NXY_EXE%" set "NXY_NEED=1"
if defined NXY_NEED if not exist "%NXY_DIR%\bin\%NXY_VERSION%" mkdir "%NXY_DIR%\bin\%NXY_VERSION%" 2>nul
if defined NXY_NEED if not exist "%NXY_DIR%\bin\%NXY_VERSION%" set "NXY_FAIL=cannot create %NXY_DIR%\bin\%NXY_VERSION%"
if defined NXY_NEED if not defined NXY_FAIL curl --version >nul 2>nul || set "NXY_FAIL=curl not found, cannot download the binary"
set "NXY_LOCK=%NXY_EXE%.lock"
set "NXY_OWN="
rem Temp files left by dead runs (a day old or more).
if defined NXY_NEED if not defined NXY_FAIL forfiles /p "%NXY_DIR%\bin\%NXY_VERSION%" /m *.tmp /d -1 /c "cmd /c del @path" >nul 2>nul
if defined NXY_NEED if not defined NXY_FAIL forfiles /p "%NXY_DIR%\bin\%NXY_VERSION%" /m *.sums /d -1 /c "cmd /c del @path" >nul 2>nul
rem A lock older than a minute belongs to a dead run (the download itself is capped at 8 s).
if defined NXY_NEED if not defined NXY_FAIL if exist "%NXY_LOCK%" powershell -NoProfile -NonInteractive -Command "if (((Get-Date) - (Get-Item -LiteralPath $env:NXY_LOCK).LastWriteTime).TotalSeconds -gt 60) { Remove-Item -LiteralPath $env:NXY_LOCK -Force -Recurse }" <nul >nul 2>nul
if defined NXY_NEED if not defined NXY_FAIL mkdir "%NXY_LOCK%" 2>nul && set "NXY_OWN=1"
if defined NXY_NEED if not defined NXY_FAIL if not defined NXY_OWN set "NXY_FAIL=downloading its binary in another hook - this hook was skipped"
if defined NXY_FAIL if not defined NXY_OWN set "NXY_SOFT=1"
if defined NXY_OWN if exist "%NXY_EXE%" set "NXY_NEED="
rem 8 s in total (6 + 2), well under the 10 s hook timeout.
if defined NXY_NEED if not defined NXY_FAIL curl -fsSL --connect-timeout 3 --max-time 6 -o "%NXY_TMP%" "%NXY_URL%/%NXY_ASSET%" <nul || set "NXY_FAIL=download of %NXY_ASSET% failed"
if defined NXY_NEED if not defined NXY_FAIL curl -fsSL --connect-timeout 3 --max-time 2 -o "%NXY_SUMS%" "%NXY_URL%/checksums.txt" <nul || set "NXY_FAIL=download of checksums.txt failed"
rem The checksum comes from the same release as the binary: it catches corruption, not a compromised release.
if defined NXY_NEED if not defined NXY_FAIL for /f "usebackq tokens=1,2" %%a in ("%NXY_SUMS%") do if "%%b"=="%NXY_ASSET%" set "NXY_WANT=%%a"
if defined NXY_NEED if not defined NXY_FAIL for /f "skip=1 delims=" %%h in ('certutil -hashfile "%NXY_TMP%" SHA256') do if not defined NXY_GOT set "NXY_GOT=%%h"
if defined NXY_NEED if not defined NXY_FAIL if defined NXY_GOT set "NXY_GOT=%NXY_GOT: =%"
if defined NXY_NEED if not defined NXY_FAIL if /i not "%NXY_GOT%"=="%NXY_WANT%" set "NXY_FAIL=checksum mismatch for %NXY_ASSET%"
if defined NXY_NEED if not defined NXY_FAIL if exist "%NXY_EXE%" del "%NXY_TMP%" 2>nul
if defined NXY_NEED if not defined NXY_FAIL if not exist "%NXY_EXE%" move /y "%NXY_TMP%" "%NXY_EXE%" >nul 2>nul
if defined NXY_NEED if not defined NXY_FAIL if not exist "%NXY_EXE%" set "NXY_FAIL=cannot install %NXY_EXE%"
if defined NXY_NEED del "%NXY_TMP%" "%NXY_SUMS%" 2>nul
if defined NXY_OWN rmdir "%NXY_LOCK%" 2>nul
if not defined NXY_FAIL if not exist "%NXY_RUN%" set "NXY_FAIL=binary not found at %NXY_RUN%"
rem Failures must be visible: stdout JSON {"systemMessage"} is shown to the user (stderr is not). Delayed expansion keeps & and ) in paths harmless.
if defined NXY_FAIL setlocal EnableDelayedExpansion
if defined NXY_FAIL echo nxy: !NXY_FAIL! 1>&2
if defined NXY_FAIL set "NXY_FAIL=!NXY_FAIL:\=/!"
if defined NXY_FAIL if defined NXY_SOFT echo {"systemMessage":"nxy: !NXY_FAIL!"}
if defined NXY_FAIL if not defined NXY_SOFT echo {"systemMessage":"nxy: !NXY_FAIL! - nxy hooks do nothing until this is fixed"}
if defined NXY_FAIL exit /b 0
"%NXY_RUN%" hook
set "NXY_CODE=%ERRORLEVEL%"
rem 9009 not found, 193 / 216 not a valid executable for this Windows: the binary could not run at all.
set "NXY_BAD="
if "%NXY_CODE%"=="9009" set "NXY_BAD=1"
if "%NXY_CODE%"=="193" set "NXY_BAD=1"
if "%NXY_CODE%"=="216" set "NXY_BAD=1"
if defined NXY_BAD setlocal EnableDelayedExpansion
if defined NXY_BAD set "NXY_RUN=!NXY_RUN:\=/!"
if defined NXY_BAD echo nxy: could not run !NXY_RUN! (exit %NXY_CODE%) 1>&2
if defined NXY_BAD echo {"systemMessage":"nxy: could not run !NXY_RUN! (exit %NXY_CODE%) - nxy hooks do nothing until this is fixed"}
if defined NXY_BAD exit /b 0
exit /b %NXY_CODE%
BATCH

# nxy shim, sh half (Linux, macOS, Git Bash). Sourced with `.`: the last line exits the shell.
NXY_VERSION=2.0.0-dev

# A failure must be visible: the stdout JSON is shown to the user by Claude Code, stderr is not.
nxy_say() {
  echo "nxy: $*" >&2
  msg=$(printf '%s' "$*" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
  printf '{"systemMessage":"nxy: %s - nxy hooks do nothing until this is fixed"}\n' "$msg"
}

# A held download lock is transient (another hook is fetching the binary), not a broken install.
nxy_skip() {
  echo "nxy: $*" >&2
  msg=$(printf '%s' "$*" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
  printf '{"systemMessage":"nxy: %s"}\n' "$msg"
}

# Download $asset into $tmp and install it as $bin. Uses the caller's variables; the caller cleans up.
# 8 s in total (6 + 2), well under the 10 s hook timeout.
nxy_fetch() {
  if ! curl -fsSL --connect-timeout 3 --max-time 6 -o "$tmp" "$base/$asset" </dev/null; then
    nxy_say "download of $asset failed"; return 1
  fi
  if ! curl -fsSL --connect-timeout 3 --max-time 2 -o "$sums" "$base/checksums.txt" </dev/null; then
    nxy_say "download of checksums.txt failed"; return 1
  fi
  # The checksum comes from the same release as the binary: it catches corruption, not a compromised release.
  want=
  while read -r h f; do
    [ "$f" = "$asset" ] && want=$h
  done < "$sums"
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$tmp" | cut -d' ' -f1)
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$tmp" | cut -d' ' -f1)
  else
    nxy_say "no sha256sum or shasum, cannot verify the binary"; return 1
  fi
  if [ -z "$want" ] || [ "$got" != "$want" ]; then
    nxy_say "checksum mismatch for $asset"; return 1
  fi
  chmod +x "$tmp" 2>/dev/null
  if [ -f "$bin" ]; then
    return 0
  fi
  if ! mv "$tmp" "$bin" 2>/dev/null && [ ! -f "$bin" ]; then
    nxy_say "cannot install $bin"; return 1
  fi
  return 0
}

nxy_main() {
  if [ -n "${NXY_BIN:-}" ]; then
    bin=$NXY_BIN
  else
    case "$(uname -s)" in
      MINGW*|MSYS*|CYGWIN*) os=windows; ext=.exe ;;
      Linux) os=linux; ext= ;;
      Darwin) os=darwin; ext= ;;
      *) nxy_say "unsupported OS $(uname -s)"; return 0 ;;
    esac
    case "$(uname -m)" in
      x86_64|amd64) arch=amd64 ;;
      aarch64|arm64) arch=arm64 ;;
      *) nxy_say "unsupported architecture $(uname -m)"; return 0 ;;
    esac
    home=${NXY_HOME:-$HOME/.nxy}
    bin=$home/bin/$NXY_VERSION/nxy$ext
    if [ ! -f "$bin" ]; then
      command -v curl >/dev/null 2>&1 || { nxy_say "curl not found, cannot download the binary"; return 0; }
      base=${NXY_RELEASE_URL:-https://github.com/mananos/nxy.dev/releases/download/v$NXY_VERSION}
      asset=nxy_${NXY_VERSION}_${os}_${arch}$ext
      mkdir -p "$home/bin/$NXY_VERSION" 2>/dev/null || { nxy_say "cannot create $home/bin/$NXY_VERSION"; return 0; }
      tmp=$bin.$$.tmp
      sums=$bin.$$.sums
      lock=$bin.lock
      # Temp files left by dead runs (older than a day).
      find "$home/bin/$NXY_VERSION" -maxdepth 1 \( -name '*.tmp' -o -name '*.sums' \) -mmin +1440 -exec rm -f {} + 2>/dev/null
      # A lock older than a minute belongs to a dead run (the download itself is capped at 8 s).
      if [ -d "$lock" ] && [ -n "$(find "$lock" -maxdepth 0 -mmin +1 2>/dev/null)" ]; then
        rmdir "$lock" 2>/dev/null
      fi
      if ! mkdir "$lock" 2>/dev/null; then
        nxy_skip "downloading its binary in another hook - this hook was skipped"; return 0
      fi
      ok=0
      if [ ! -f "$bin" ]; then
        nxy_fetch || ok=1
      fi
      rm -f "$tmp" "$sums"; rmdir "$lock" 2>/dev/null
      [ "$ok" = 0 ] || return 0
    fi
  fi
  if [ ! -f "$bin" ]; then
    nxy_say "binary not found at $bin"
    return 0
  fi
  "$bin" hook
  rc=$?
  # 126 / 127: the binary could not run at all (not executable, wrong format, not found).
  if [ "$rc" = 126 ] || [ "$rc" = 127 ]; then
    nxy_say "could not run $bin (exit $rc)"
    return 0
  fi
  return $rc
}

nxy_main
exit $?
