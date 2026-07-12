# claude-swap Go port: design

Status: draft for review
Date: 2026-07-11
Author: port effort (Go rewrite of realiti4/claude-swap)

## Goal

Port the Python `claude-swap` CLI (the `cswap` tool) to Go, preserving
behavior and CLI surface so the result is a drop-in replacement, while gaining
single-binary distribution, faster startup, and a concurrent daemon. The port
is optimized for side-by-side trackability with upstream Python: a future
Python commit should map onto a Go file and function with minimal searching, so
that new upstream features land in the Go port quickly.

The guiding principle is **port, not fork**: structure, names, method sets, and
file boundaries mirror the Python source wherever Go allows, even when more
idiomatic Go layouts exist. Deliberate divergences are called out explicitly in
the "Divergences from upstream" section and kept as small and localized as
possible.

## Scope

In scope for v1:

- Core CLI: `add`, `switch`, `list`, `status`, `remove`, `run` (session mode),
  `config`, `purge`, `upgrade`, and original flag-spelling aliases.
- Auto-switch daemon: `cswap auto` foreground loop, `--once`, `--dry-run`,
  `--json`, all `autoswitch.*` settings.
- Cross-platform credential storage from day one: macOS (Keychain via the
  `security` CLI), Windows (file-based `.enc`), Linux/WSL (file-based `.enc`).
- Credential-provenance safety logic (unclaimed-credential stash, `.prev`
  one-generation retention, mutual-exclusion of OAuth vs managed API key).

Out of scope for v1 (deferred to later phases, see "Future phases"):

- The full-screen Textual-equivalent TUI dashboard (`cswap tui` / `cswap watch`
  / bare `cswap`). v1 bare-`cswap` prints help with a note that the dashboard
  is coming.
- The macOS menu bar app (`cswap menubar`).

## Language and rationale

Go. Reasons, in priority order for this specific tool:

1. **Frequently invoked, latency-sensitive.** `cswap` runs on every switch, every
   `list`, and every daemon poll. Go's compiled, statically-linked binary starts
   in milliseconds with no runtime to load; this matters because startup cost is
   paid on every invocation.
2. **Single-binary distribution, no runtime dependency.** `GOOS`/`GOARCH`
   cross-compilation produces one static binary per target. Users do not need
   Python, `uv`, or `pipx` installed.
3. **Library fit.** Credential storage needs only `os/exec` (the `security` CLI)
   and stdlib file I/O (see "Dependency map"). The daemon's concurrency maps
   cleanly onto goroutines. No exotic or unmaintained dependencies are required.
4. **Maintainability over a long-lived systems CLI.** Static typing, `gofmt`,
   `go vet`, and a tiny toolchain keep the codebase legible as it tracks upstream.

This is the well-trodden path for cross-platform systems CLIs (`gh`, `docker`,
`kubectl`, `k9s`, `hugo`).

JS/TS (Bun compiled binary, or npm-distributed Node) was considered and
rejected: larger binaries, a weaker/depcrecated native keychain story (`keytar`
is unmaintained, forcing manual shell-out anyway), and (for Node) worse cold
start plus a runtime dependency.

## Repo layout

- `main` branch: pure upstream mirror, byte-identical to `realiti4/claude-swap`
  `main`. An `upstream` git remote is configured so `git fetch upstream && git
  log upstream/main` shows new commits to port. The Python source under
  `src/claude_swap/` is never modified on `main`.
- `go-port` branch: all Go code and this design doc live here. Branching (rather
  than a `go/` subdirectory on `main`) keeps `git diff upstream/main` clean: the
  Python files show no changes, so new upstream commits surface immediately.

## Package and file structure

One Go package, `internal/cswap`, with one file per Python module, same names
where Go casing rules allow. A single package (rather than sub-packages like
`creds/`, `lock/`) mirrors Python's free cross-module referencing and sidesteps
Go's stricter import-cycle rules, keeping the file-for-file mapping honest.

```
cswap/
  cmd/cswap/main.go              <- __main__.py   (thin entrypoint)
  internal/cswap/
    autoswitch.go                <- autoswitch.py
    cache.go                     <- cache.py
    claude_locks.go              <- claude_locks.py
    cli.go                       <- cli.py
    credentials.go               <- credentials.py
    errors.go                    <- exceptions.py
    json_output.go               <- json_output.py
    locking.go                   <- locking.py
    logging_config.go            <- logging_config.py
    macos_keychain.go            <- macos_keychain.py
    migrations.go                <- migrations.py
    models.go                    <- models.py
    oauth.go                     <- oauth.py
    paths.go                     <- paths.py
    printer.go                   <- printer.py
    process_detection.go         <- process_detection.py
    session.go                   <- session.py
    settings.go                  <- settings.py
    snapshot_source.go           <- snapshot_source.py
    switcher.go                  <- switcher.py  (core + CRUD; see split note)
    switcher_add.go              <-   (add_account, add_account_from_token)
    switcher_switch.go           <-   (switch, switch_to, _perform_switch, classification, stash)
    switcher_usage.go            <-   (_fetch_*_usage, _collect_usage_entries, warnings)
    switcher_list.go             <-   (list_accounts, status, payload builders)
    switcher_session.go          <-   (session-mode helpers)
    transfer.go                  <- transfer.py
    update_check.go              <- update_check.py
    usage_store.go               <- usage_store.py
  go.mod
  go.sum
```

The 3,817-line `switcher.py` is one class. It is too large for one readable Go
file, so its methods are split across sibling `switcher_*.go` files by the
existing internal sections. All files are the same package, the same struct,
with identical method names and signatures. A reader looking for
`_perform_switch` finds it in `switcher_switch.go`. This split is organization
only; it does not change the method set.

Deferred files (TUI phase): `tui/` package, `menubar.go`.

## Naming and error conventions

- Identifiers stay as close to the Python names as Go casing allows.
  `ClaudeAccountSwitcher` stays `ClaudeAccountSwitcher` (a struct); its methods
  become `func (s *ClaudeAccountSwitcher) Switch(...)`. Data types
  (`AccountInfo`, `AccountSnapshot`, `AccountsSnapshot`, `SwitchTransaction`,
  `AutoSwitchEngine`) keep their exact names. snake_case Python functions become
  camelCase/PascalCase with the same words in the same order
  (`get_timestamp` -> `GetTimestamp`; private helpers like `_clamped` stay
  unexported, e.g. `clamped`, matching their private status in Python).
- `exceptions.py`'s class hierarchy has no direct Go equivalent (no inheritance),
  so `errors.go` mirrors it with wrapped error types. Each Python exception class
  becomes a Go error type whose `Unwrap() error` returns a shared base sentinel,
  so `errors.Is(err, cswap.ErrCredentialError)` matches whether the concrete
  error is `CredentialReadError` or `CredentialWriteError`. This preserves the
  catch-by-category behavior of Python's `except CredentialError`.

## Dependency map

External dependencies (three total):

- `github.com/spf13/cobra` and `github.com/spf13/pflag` for the CLI layer.
- `github.com/gofrs/flock` for cross-platform advisory file locking (wraps
  `syscall.Flock` on Unix and `LockFileEx` on Windows). Used by `locking.go`.

Everything else is stdlib. Notable mappings:

- **`keyring` (Python) is not needed.** `credentials.py` shells out to the macOS
  `security` CLI directly (via `macos_keychain.py`) and stores per-account
  backups as base64 `.enc` files on all platforms. The `keyring` dependency in
  `pyproject.toml` appears only in `_sweep_legacy_keyring`, a one-time cleanup.
  In the Go port, `macos_keychain.go` calls `security` via `os/exec`
  (`add-generic-password`, `find-generic-password`, `delete-generic-password`),
  and file backends use `encoding/base64` plus atomic `os.Rename`. The legacy
  keyring sweep is ported as a best-effort no-op pass on first run.
- **`truststore` (Python) is not needed.** Go's `crypto/tls` reads the platform
  trust store natively (System keychain on macOS, CA bundle on Linux, system
  roots on Windows).
- **`proper-lockfile` (npm, used by Claude Code itself) is re-implemented by
  hand.** `claude_locks.go` reproduces the directory-mkdir atomicity + mtime
  liveness protocol (stale after 10s, holder touches mtime every 5s) with pure
  stdlib `os` calls (`Mkdir`, `os.Stat`, `Chtimes`).
- JSON: `encoding/json`.

## Credential storage and locking layer

`credentials.go`'s `CredentialStore` ports method-for-method. The two storage
backends (macOS Keychain via `security`, base64 `.enc` files) and their routing
rules port verbatim, because they encode subtle correctness properties:

- `.enc`-wins reads on macOS: a fallback `.enc` (written while the Keychain was
  unusable) is authoritative over a possibly-stale Keychain copy. A successful
  Keychain write reconciles the `.enc` away (correctness-critical, not
  best-effort).
- Sticky per-process Keychain capability cache with a bounded re-probe cooldown
  (`KEYCHAIN_RECHECK_COOLDOWN_S`), so one CLI invocation cannot split-brain
  between backends, but a long-running daemon re-probes after a transient
  `security` timeout.
- Pin-to-file-mode after a write fallback, so a stale Keychain entry cannot be
  resurrected by a later cooldown re-probe.
- Mutual exclusion of OAuth vs managed API key on the active store, mirroring
  Claude Code's own `saveApiKey`/`removeApiKey`.
- The unclaimed-credential stash: append-only base64 `.enc` entry files plus a
  JSON manifest carrying classification evidence. These are deliberately 0600
  files on every platform (outside the Keychain even on macOS), because a failed
  safety-copy write must abort the switch and must not inherit the Keychain's
  flaky failure modes.
- `.prev` one-generation retention per slot, routed by the same rule as the
  backup itself (Keychain when usable, `.enc.prev` file otherwise), so retention
  never weakens the user's storage posture.

`locking.go`'s `FileLock` wraps `gofrs/flock`. `claude_locks.go` reproduces the
cooperation protocol with Claude Code's own advisory locks
(`~/.claude.lock`, `~/.claude.json.lock`): hold the directory-mutex lock while
mutating Claude Code's credential/config files, so a swap never interleaves with
a token refresh.

## Switcher engine

`ClaudeAccountSwitcher` becomes a Go struct holding the same state (platform,
paths, credential store, logger, sequence data) and exposing the same method
set. The `SwitchTransaction` (with its reverse-order rollback) ports to a struct
method. The safety-critical provenance logic ports character-for-character:

- `_classify_outgoing_credential`: decide whether the live credential belongs to
  the outgoing slot, a different known slot, or nobody.
- `_stash_live_credential`: preserve live credential bytes attributed to someone
  other than the outgoing slot (invariant: never overwrite the live store
  without preserving what was in it; the bytes may be the only live copy of some
  account's refresh token).

`process_detection.go` reads Claude Code's session PID files
(`~/.claude/sessions/{pid}.json`) and IDE lockfiles (`~/.claude/ide/{port}.lock`)
to detect running instances, using the same mechanism Claude Code uses
internally.

`oauth.go` implements token refresh against
`https://platform.claude.com/v1/oauth/token` (client id
`9d1c250a-e61b-44d9-88ed-5944d1962f5e`, the `oauth-2025-04-20` beta header) and
the usage API. HTTP calls go through an injectable `Doer` interface so tests
need no network.

`usage_store.go` and `cache.go` port the usage read model (`UsageEntry` with
staleness/age tracking and last-good sentinel states).

## Auto-switch daemon

The daemon is an `AutoSwitchEngine` struct with a sequential tick loop:
`RunLoop` -> `Tick` -> `_tick_inner` -> `_next_delay` -> `time.Sleep` -> repeat.
The decision logic (binding-window percentage, threshold, cooldown, hysteresis
margin, quarantine of dead refresh tokens, "sleep until first reset" when all
accounts are exhausted) ports verbatim from `autoswitch.py`. State
(`autoswitch_state.json`: cooldown, quarantined accounts) is persisted under the
same `FileLock` as the Python tool.

The 9 `AutoSwitchEvent` subclasses (`PollEvent`, `SwitchEvent`, `NoSwitchEvent`,
`QuarantineEvent`, `UnquarantineEvent`, `AllExhaustedEvent`, `SleepEvent`,
`ErrorEvent`, `ConfigWarningEvent`) become Go structs implementing a common
interface: `Fields() map[string]any`, `Human() string`, `ToJSON() map[string]any`.
`--json` emits one event per line; `--once` exit codes match the documented
contract (0 switched, 1 error, 2 nothing-to-do, 3 blocked/no viable target).

The `TickOutcome` enum and `binding_pct`/window-percentage helpers port
directly.

### Concurrency divergence (intentional)

The upstream Python daemon polls usage sequentially, deliberately a couple of
accounts per check, to keep API traffic flat. This port diverges in one
localized place: **within a tick, usage fetches for the accounts scheduled to be
polled that tick fan out concurrently using goroutines** (a `sync.WaitGroup` or
`errgroup.Group`), rather than sequentially. The tick loop itself remains
sequential (tick, decide, sleep), and the adaptive poll-plan logic (which
accounts are due, busy accounts watched more closely, exhausted ones left alone)
is otherwise unchanged. This is a performance optimization chosen for the port;
it trades the upstream's strictly flat traffic shape for lower per-tick latency
when many accounts are managed. See "Divergences from upstream".

## CLI surface (cobra)

The cobra command tree mirrors the subcommands one-to-one. Each command maps to
a method on `ClaudeAccountSwitcher` (or `AutoSwitchEngine` for `auto`), with
output via `printer.go` (human) and `json_output.go` (`--json`).

| Command | Flags / notes |
| --- | --- |
| `cswap add` | `--slot N`, `-y`/`--assume-yes` |
| `cswap switch [identifier]` | `--strategy best|next-available`; bare = rotate |
| `cswap list` | the dashboard-table output |
| `cswap status` | `--json` |
| `cswap remove <identifier>` | `-y` |
| `cswap auto` | `--threshold`, `--interval`, `--cooldown`, `--model`, `--once`, `--dry-run`, `--json` |
| `cswap config` | `get <key>`, `set <key> <value>`, `unset <key>`, `list` |
| `cswap run <identifier>` | session mode; args after `--` forwarded to `claude`; `--share-history` |
| `cswap purge` | remove all claude-swap data |
| `cswap upgrade` | self-upgrade |
| bare `cswap` | v1: print help with a note that the dashboard is coming (TUI deferred) |

Original flag spellings (`cswap --switch`, `cswap --list`, ...) are kept as
hidden cobra aliases for parity with the documented compatibility behavior.

The `settings.py` `SettingSpec` registry (the single source of truth for bounds,
choices, defaults, and help text, used by both the lenient load-time clamp and
the strict `config set` validation) ports to `settings.go` so the two validation
paths cannot drift, exactly as in Python.

## Testing

Go's `testing` package, table-driven, mirroring the upstream `tests/` directory
structure one-to-one. Substitutability is via dependency injection:

- `macos_keychain.go`'s `security` subprocess calls go through a small interface,
  so tests inject a fake runner (the Go equivalent of the Python suite's autouse
  in-memory Keychain guard).
- The OAuth/usage HTTP client goes through the `Doer` interface, so usage-fetch
  logic is tested with canned responses and no network.

Priority for v1 test coverage: the credential-provenance classification and
stash logic (the safety-critical path), the switch transaction rollback, the
settings validation/clamp round-trips, and the daemon's threshold/hysteresis
decision cases.

## Build and release

- `go build` produces a single static binary for the host.
- Cross-compile matrix: `darwin/amd64`, `darwin/arm64`, `linux/amd64`,
  `linux/arm64`, `windows/amd64`, `windows/arm64`.
- A `goreleaser` config builds the matrix and publishes GitHub Releases under the
  fork, mirroring how `uv tool install` distributes the original. A Homebrew tap
  is a later option.

## Divergences from upstream

Explicit, intentional differences (everything else is a faithful port):

1. **Daemon usage polling is concurrent within a tick** (goroutines), where
   upstream is sequential couple-per-tick. Localized to the usage-fetch method.
   Rationale: performance, per the port's goal.
2. **Bare `cswap` prints help** instead of opening the TUI, because the TUI is
   deferred. Temporary; removed when the TUI phase lands.
3. **`switcher.py` is split across `switcher_*.go` files** for readability;
   organization only, method set unchanged.
4. **The legacy `keyring` sweep is a best-effort no-op on first run**, since the
   Go port has no `keyring` dependency to sweep.
5. **Language-level error model** uses wrapped sentinel errors instead of a
   class hierarchy, preserving catch-by-category semantics via `errors.Is`.

## Future phases

- **TUI dashboard** (`cswap tui`, `cswap watch`, bare `cswap`). Planned framework:
  Charm's Bubble Tea + Bubbles + Lipgloss, the de facto Go TUI stack. The
  switcher/usage/autoswitch packages are designed as importable libraries so the
  TUI consumes them directly without a rewrite. `AccountSnapshot` /
  `AccountsSnapshot` are the read models the TUI renders.
- **macOS menu bar app** (`cswap menubar`), wrapping the same engine.
