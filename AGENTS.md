# Agents

## Code Hooks

Hook repository: `https://github.com/mose-x/code-hooks`

The hooks (pre-commit identity + lang fmt/lint, commit-msg message rules, pre-push branch + identity + tag validation) are the single source of truth; CI commit-lint is the backstop. Do not bypass with `--no-verify` unless a ref deletion/force-push is truly unavoidable (the hook can't process those — see Caveats below).

Key rules (full rules take precedence from the hook repo):
- Both commit author **and** committer emails must be on the allowlist (`602187256@qq.com`).
- Total commit message length must NOT exceed 200 characters.
- Conventional Commits format (`feat: ...`, `fix(scope): ...`, `docs: ...`, `test: ...`).
- Subject ASCII only, <=100 chars. No forbidden tokens (`co-authored by`, `trae agent`, etc.).
- Allowed push branch: `dev` only. `main` is protected, must go through PR.

## Branches

- `main` — release branch. A `vX.Y.Z` tag push triggers `build.yml` (6-platform build).
- `dev` — the only pushable working branch. `[allowed_branches]` in `hook-rules.conf` is `dev` only. No other branch can be pushed and `main` cannot be pushed directly.

## Unit Test Requirement

**All new code, bug fixes, and refactors MUST include unit tests. No exceptions.**

- Every new function, bug fix, or behavior change requires at least one test.
- Tests should cover: **happy path, edge cases, and error paths**.
- Go tests live alongside the code: `*_test.go` in the same package.
- Use `t.TempDir()` for filesystem-touching logic (fake SVC home via `config.Config{}.SetHomeDir(dir); cfg.SetSvcDir(dir)`); **never touch the real `~/.svc/` in tests**.
- Tests must be platform-aware where the code is (use `runtime.GOOS`/`runtime.GOARCH` for .exe vs bare-name, hardlink vs .cmd wrapper) so they pass on all 3 CI OSes (windows/macos/linux).
- Pure-logic functions (parsers, mappers, alias maps, version comparison) get pure-logic tests (no filesystem). Extract arch-dependent logic into pure functions taking `(goos, goarch string)` params so all 6 platform combos are testable on any host.
- Shell script changes require syntax checks (`bash -n`) at minimum; content-verification tests are encouraged.
- PRs without tests for their code changes will NOT be merged.

### Existing test locations

- SDK fetchers: `internal/sdk/*_test.go` (rust, dotnet, flutter, perl, php, android, jdk, nodejs, dart, endpoint)
- Shim system: `internal/shim/shim_test.go` + `internal/shimmanager/manager_test.go` + `rcfile_unix_test.go`
- Install lifecycle: `internal/installer/*_test.go` (residual-dir filter, reinstall wait, fetcher locks, checksum, extractor failure)
- Import flows: `internal/importer/importer_test.go` (atomic copy, layout alignment, critical files)
- Storage/migration: `internal/storage/*_test.go`
- Updater: `internal/update/*_test.go` (script rendering, sha256, ParseAppInfo)
- Settings/proxy/helpers/pkgmgr/logmgr/wailsrt: `internal/<pkg>/*_test.go`
- Installer template: `internal/packaging/project_nsi_test.go`
- RC file (Unix): `internal/shimmanager/rcfile_unix_test.go`

## TODO Tracking

When a fix or feature addresses an item documented in a tracking file (e.g. `note/todo.md`), the **same commit** (or PR) that fixes the code MUST also update the item's status in the tracking file (e.g. change `unfixed` to `fixed (#PR)`). This keeps tracking files in sync with the codebase — no stale unfixed items for already-fixed bugs.

Rules:
- If the tracking file is in `.gitignore` (like `note/`), update it locally but do NOT `git add` it — the status is for local reference only.
- If the tracking file IS tracked by git, `git add` it together with the code fix so both land in the same commit.
- Update status to include the PR number or commit hash for traceability (e.g. `fixed (#74)`).

## Why Recreate `dev` Each Cycle

**Always create a fresh `dev` branch from `main` for each development cycle. Never reuse an old `dev` or rebase `dev` onto `main`.**

`main`'s squash-merge commits have `committer = GitHub <noreply@github.com>`. The pre-push identity scan rejects any noreply-committer commit inside `dev`'s push range. If `dev` is rebased onto or merged with `main`, the noreply squash commits enter `dev`'s push range and pre-push fails.

Creating `dev` fresh from `main` each cycle keeps the push range = only the new commits (main's noreply squashes sit on `origin/main`, not counted as "new" and not scanned), so pre-push passes.

## Caveats

- `git push --delete <branch>` and force-pushes are rejected by the pre-push hook (it errors on `new=0000` ref deletions and on noreply in a force-push range). Use the GitHub API (`gh api -X DELETE repos/mose-x/sdk-version-control/git/refs/heads/dev`) or `gh release delete --cleanup-tag` for tags.
- CI merge race: after CI completes, `mergeStateStatus` may show `BLOCKED` for ~10-30s before flipping to `CLEAN` (the push-event run has `commit-lint` skipped; the PR-event run passes). Poll `gh pr view <n> --json mergeStateStatus` until `CLEAN` before merging.

## Development Workflow (Mandatory)

Every code change MUST follow this exact sequence. No steps may be skipped.

### Step 1: Branch
```bash
git checkout main && git fetch origin && git merge --ff-only origin/main
git checkout -b dev   # only "dev" is allowed by pre-push hook
```

### Step 2: Code + Tests
- Write the fix/feature code.
- Write unit tests covering happy path + edge cases + error paths.
- Extract arch-dependent logic into pure functions taking `(goos, goarch string)` for cross-platform testability.
- If modifying shell scripts (`scripts/*.sh`), run `bash -n` for syntax + ensure LF line endings (`.gitattributes` forces `*.sh` to LF).

### Step 3: Local Verification (MUST pass before commit)
```bash
gofmt -w .                    # auto-format
go vet ./...                  # static analysis
go build ./...                # compile check
go test ./...                 # ALL tests must pass
```

For CRLF check on Go files (Windows autocrlf artifacts):
```bash
for f in <changed .go files>; do tr -d '\r' < "$f" > /tmp/x.lf && gofmt -l /tmp/x.lf; done
# (empty output = clean)
```

**If `go test ./...` fails, do NOT proceed. Fix the failing test first.**
**Do NOT use `--no-verify` to bypass hooks — fix the root cause.**

### Step 4: Commit
```bash
git add <specific files>   # never `git add -A` or `git add .`
git commit -m "fix(scope): description"
```

The pre-commit hook will run `go vet` on staged Go files. If the hook rejects, fix the issue and re-commit.

### Step 5: Push
```bash
# Delete old remote dev if it exists (from previous PRs):
# Use gh API (not git push --delete — the hook can't handle ref deletions):
HTTPS_PROXY=http://127.0.0.1:7890 gh api -X DELETE repos/mose-x/sdk-version-control/git/refs/heads/dev 2>/dev/null || true

# Push with proxy (Clash on port 7890 — github.com is blocked without proxy):
HTTPS_PROXY=http://127.0.0.1:7890 HTTP_PROXY=http://127.0.0.1:7890 git push -u origin dev
```

The pre-push hook will scan the push range + run the per-language `test` stage (`go test`).

### Step 6: PR + CI
```bash
GH="/c/Users/mose/AppData/Local/Microsoft/WinGet/Packages/GitHub.cli_Microsoft.Winget.Source_8wekyb3d8bbwe/bin/gh.exe"
HTTPS_PROXY=http://127.0.0.1:7890 HTTP_PROXY=http://127.0.0.1:7890 "$GH" pr create --base main --head dev --title "..." --body "..."
```

CI runs on the PR (`.github/workflows/ci.yml`):
1. `gofmt -l .` + `go vet ./...` (Linux)
2. `go build ./...` + `go test ./...` (Ubuntu, Windows, macOS — 3 OSes)
3. `commit-lint` (PR only — checks conventional commits + identity)

**ALL checks must pass (green). Do NOT merge if any check fails or is pending.**

### Step 7: Merge
```bash
# Poll mergeStateStatus until CLEAN (avoids the push/PR event race):
for i in $(seq 1 10); do
  S=$(HTTPS_PROXY=http://127.0.0.1:7890 "$GH" pr view <PR_N> --json mergeStateStatus --jq .mergeStateStatus)
  echo "[$i] $S"
  case "$S" in CLEAN|BEHIND) break;; esac
  sleep 10
done
HTTPS_PROXY=http://127.0.0.1:7890 "$GH" pr merge <PR_N> --squash
```

### Step 8: Cleanup
```bash
# Delete remote dev (via API — git push --delete is rejected by the hook):
HTTPS_PROXY=http://127.0.0.1:7890 "$GH" api -X DELETE repos/mose-x/sdk-version-control/git/refs/heads/dev
# Sync local main:
HTTPS_PROXY=http://127.0.0.1:7890 git fetch origin
git checkout main && git merge --ff-only origin/main
git branch -D dev
```

### Step 9: Tag (only if releasing a new version)
```bash
# Delete old tag if re-tagging (via gh release delete --cleanup-tag, server-side):
HTTPS_PROXY=http://127.0.0.1:7890 "$GH" release delete v<X.Y.Z> --cleanup-tag --yes
# Re-create tag at current main HEAD:
git tag -a v<X.Y.Z> -m "Release vX.Y.Z: ..."
HTTPS_PROXY=http://127.0.0.1:7890 git push origin v<X.Y.Z>
```

Tagging triggers `build.yml` -> builds 6 platform assets (Win x64/arm64, macOS x64/arm64, Linux x64/arm64) + `sha256sums.txt` + creates a GitHub Release.

## Local Environment Setup (Windows)

### Prerequisites
- **Go 1.25+** (via SVC or system)
- **Node.js 18+** (via SVC or system) — for Wails frontend build
- **Wails CLI**: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **Clash proxy** on port 7890 (github.com is GFW-blocked; all git/gh commands need `HTTPS_PROXY=http://127.0.0.1:7890`)
- **gh CLI** installed via winget (`winget install GitHub.cli`) + `gh auth login` (interactive, browser flow)

### Proxy pattern
All git push/fetch and gh commands need the proxy:
```bash
HTTPS_PROXY=http://127.0.0.1:7890 HTTP_PROXY=http://127.0.0.1:7890 git push origin dev
```
GitHub-hosted CI runners are NOT behind the GFW — they don't need the proxy. Only local commands do.

### Known issues
- **CRLF**: Windows autocrlf converts .go files to CRLF in the working tree. CI (Linux) checks out LF — `gofmt -l` may show false "needs fmt" locally. Use `tr -d '\r' < file | gofmt -l` to verify on LF-normalized content. `.gitattributes` forces `*.sh` to LF (CRLF breaks bash heredocs/case on Linux/macOS).
- **`python3` on Windows**: SVC's Python shim only provides `python` (Windows CPython has no `python3.exe`). The code-hooks `commit-msg` hook uses `python3 -c` for message-length counting. SVC now creates the `python3` alias itself (PR #69/#71); if the hook still fails, a Go-compiled `python3.exe` wrapper can be placed at `~/.svc/shims/python3.exe` (delegates to the `python` shim).
- **Merge race**: after CI completes, `mergeStateStatus` may show `BLOCKED` for 10-30s before flipping to `CLEAN`. Poll before merging (see Step 7).
- **`--no-verify`**: never use for normal commits/pushes. Only for ref deletions/force-pushes that the hook can't process. Prefer the GitHub API (`gh api -X DELETE`) for branch/tag deletion.
- **Dev branch stale commits**: if `dev` accumulates stale commits from prior PRs, the merge-base goes stale -> add/add conflicts. Always recreate `dev` fresh from `main` each cycle (Step 1).
