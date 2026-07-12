# claude-swap (Go port)

A Go port of [claude-swap](https://github.com/realiti4/claude-swap), the
multi-account switcher for Claude Code. All credit for the original tool and
its design belongs to the upstream author, Onur Cetinkol. This fork is a
from-scratch Go reimplementation that tracks upstream and aims to be a
behavior-compatible drop-in: single static binary, same account model, same
on-disk layout.

> **Status: usable.** The full CLI surface is ported and released: account
> management, the transactional switch core, the auto-switch daemon,
> export/import, self-update checks, session mode, the interactive TUI
> dashboard, and the macOS menu bar app. See [Roadmap](#roadmap). The
> `v0.20.0` release has gone through a follow-up parity audit against
> upstream: dozens of small CLI, TUI, and menu bar behavior gaps fixed,
> plus one high-impact bug where per-model usage windows could silently
> drop for any store-served data (affecting auto-switch decisions).

## Install

### macOS / Linux (one-liner)

```bash
curl -fsSL "https://github.com/mliem2k/claude-swap/releases/latest/download/cswap_$(uname -s | tr '[:upper:]' '[:lower:]')_$(test "$(uname -m)" = x86_64 && echo amd64 || echo arm64).tar.gz" | tar -xz -C /usr/local/bin
```

Downloads the right binary for your OS/arch from the
[latest release](https://github.com/mliem2k/claude-swap/releases/latest) and
extracts it straight into `/usr/local/bin`. If that directory isn't
writable, prepend `sudo` or extract into a directory already on your `PATH`
(e.g. `-C ~/.local/bin`).

Verify:

```bash
cswap --version
```

### Windows (one-liner, PowerShell)

```powershell
Invoke-WebRequest -Uri "https://github.com/mliem2k/claude-swap/releases/latest/download/cswap_windows_amd64.zip" -OutFile "$env:TEMP\cswap.zip"; Expand-Archive -Path "$env:TEMP\cswap.zip" -DestinationPath "$env:TEMP\cswap-extract" -Force; Move-Item -Force "$env:TEMP\cswap-extract\cswap.exe" "$env:LOCALAPPDATA\Microsoft\WindowsApps\cswap.exe"; Remove-Item -Recurse -Force "$env:TEMP\cswap.zip","$env:TEMP\cswap-extract"
```

Installs into `%LOCALAPPDATA%\Microsoft\WindowsApps`, which ships on every
account's `PATH` by default on Windows 10/11, so no PATH edits are needed.
Open a new terminal and run `cswap --version` to confirm.

If you'd rather not run a script, download `cswap_windows_amd64.zip` from the
[latest release](https://github.com/mliem2k/claude-swap/releases/latest)
manually, extract `cswap.exe`, and put it anywhere on your `PATH`.

### Uninstall (macOS / Linux)

```bash
rm -f "$(command -v cswap)"
```

Removes whatever `cswap` binary is currently first on your `PATH`. Prepend
`sudo` if it was installed somewhere requiring elevated permissions. This
only removes the binary; account data lives separately (`~/.claude-swap-backup`
on macOS, `$XDG_DATA_HOME/claude-swap` or `~/.local/share/claude-swap` on
Linux) and is untouched, run `cswap purge` first if you also want that
removed.

### Uninstall (Windows, PowerShell)

```powershell
Remove-Item -Force "$env:LOCALAPPDATA\Microsoft\WindowsApps\cswap.exe"
```

Only removes the binary; account data lives separately under
`%USERPROFILE%\.claude-swap-backup` and is untouched, run `cswap purge`
first if you also want that removed.

### From source

Requires Go 1.22 or newer.

```bash
git clone https://github.com/mliem2k/claude-swap.git
cd claude-swap
git checkout go-port
go build -o cswap ./cmd/cswap
go test ./...
```

## Commands

```
cswap add [--slot N]              register the current live login
cswap add-token                   register an API-key account
cswap list                        list managed accounts
cswap status                      show the active account's usage
cswap switch [NUM|EMAIL]          rotate, or jump to a specific account
cswap remove NUM|EMAIL            remove a managed account
cswap purge                       remove all managed accounts and backups
cswap config get|set|unset|list   view/edit auto-switch settings
cswap auto [--once] [--dry-run]   the auto-switch daemon
cswap export <path>               export accounts to a portable JSON file
cswap import <path>               import accounts from a portable JSON file
cswap run NUM|EMAIL [-- ...]      [experimental] launch Claude Code as a
                                   stored account in an isolated per-account
                                   session profile, this terminal only
cswap upgrade                     check this repo's releases and install a newer build in place
cswap tui                         interactive dashboard (also: bare cswap)
cswap watch                       dashboard, opened on the live watch page
```

Every command supports `--json` for machine-readable output and `--debug`
for verbose logging. `cswap --menubar` opens the macOS menu bar app
(bird's-eye account list, rotate/best/next-available, add/remove, refresh
credentials, switch history, settings, all backed by the same engine as
`cswap auto`/`cswap tui`) instead of dispatching to a subcommand. Legacy
`cswap --list`/`--switch`/etc. flag forms from the original Python CLI are
still accepted.

## Why a Go port

- **Single static binary**, no Python or `uv` runtime to install.
- **Fast startup**, which matters because `cswap` runs on every switch and every
  daemon poll.
- **Trivial cross-compilation** (`GOOS`/`GOARCH`) for macOS, Linux, and
  Windows, with one exception: darwin builds link Cocoa/AppKit/UserNotifications
  for the menu bar app (`CGO_ENABLED=1`), so they need an actual Mac with
  Xcode Command Line Tools, unlike the Linux and Windows builds.
- **A concurrent daemon** for usage polling, while staying a faithful port.

Measured on `cswap auto --once` (single tick, no accounts registered, same
machine, same scenario for both):

| | Go port | Python upstream | Ratio |
| --- | --- | --- | --- |
| Max RSS | 12.7 MB | 43.5 MB | 3.4x less |
| Peak memory footprint | 4.77 MB | 31.2 MB | 6.5x less |
| Wall time | 0.01s | 0.23s | 23x faster |
| Instructions retired | 54.7M | 1.19B | 21.7x fewer |

The gap is mostly Python interpreter and module-import startup cost, not
per-tick work. See [`docs/go-port-design.md`](docs/go-port-design.md) for the
full design, including the deliberate divergences from upstream.

## Repository layout

| Branch | Purpose |
| --- | --- |
| `main` | A pure mirror of `realiti4/claude-swap`. Byte-identical to upstream so new commits are easy to spot. Never modified. |
| `go-port` | All Go code, the design doc, and the plans live here. **This is the fork's default branch.** |

An `upstream` remote is configured so `git fetch upstream` surfaces new Python
commits to port. The port is organized as **port, not fork**: file names,
method sets, and structure mirror the Python source (`internal/cswap/*.go`,
one or more Go files per Python module), so a future upstream commit maps
onto a Go file and function with minimal searching.

Every implementation step is recorded as a numbered plan under
[`docs/plans/`](docs/plans/), each with its own scope, task breakdown, and
review notes.

## Roadmap

Ported: the full systems layer (errors, paths, logging, macOS Keychain,
locking, credential store), OAuth/usage tracking, the switcher (sequence
registry, add/remove, the transactional switch core with rollback), the CLI
surface, the auto-switch daemon, export/import, self-update checks, session
mode (`cswap run`), the full-screen TUI dashboard (`cswap tui`/
`cswap watch`, built on [Bubble Tea](https://github.com/charmbracelet/bubbletea)),
and the macOS menu bar app (`cswap --menubar`).

Nothing is currently deferred; the full CLI, TUI, and menu bar surfaces are
all ported.

## Divergences from upstream

Intentional and documented in the design doc. Notable ones:

- The daemon polls account usage **concurrently within a tick**
  (goroutines), where upstream polls sequentially, trading upstream's
  strictly flat traffic shape for lower per-tick latency.
- `cswap upgrade` checks this fork's own GitHub releases rather than
  upstream's PyPI/uv/pipx flow, but does actually replace the running
  binary in place once a newer release is found (a same-directory rename
  on macOS/Linux, a rename-aside-then-replace on Windows), no separate
  package manager involved.

Everything else is a faithful port.

## License

MIT, inherited from upstream. See [LICENSE](LICENSE).
