# Agents

Hook repository:
`https://github.com/mose-x/code-hooks`

The hooks (pre-commit identity + lang fmt/lint, commit-msg message rules, pre-push branch + identity + tag validation) are the single source of truth; CI commit-lint is the backstop. Do not bypass with `--no-verify` unless a ref deletion/force-push is truly unavoidable (the hook can't process those — see below).

## Branches

- `main` — release branch. A `vX.Y.Z` tag push triggers `build.yml`.
- `dev` — the only pushable working branch. `[allowed_branches]` in code-hooks `hook-rules.conf` is `dev` only, so no other branch can be pushed and `main` cannot be pushed directly.

## Testing

All code changes (new features, bug fixes, refactors) MUST include unit tests covering the new/changed behavior. A PR without tests for its code changes is incomplete and will not be merged.

- Go tests live alongside the code: `*_test.go` in the same package (e.g. `internal/shim/shim_test.go`, `internal/shimmanager/manager_test.go`).
- Use `t.TempDir()` for filesystem-touching logic (fake SVC home via `config.Config{}.SetSvcDir(t.TempDir())`); never touch the real `~/.svc` in tests.
- Tests must be platform-aware where the code is (use `runtime.GOOS`/`runtime.GOARCH` for .exe vs bare-name, hardlink vs .cmd wrapper, etc.) so they pass on all 3 CI OSes (windows/macos/linux).
- Pure-logic functions (parsers, mappers, alias maps, version comparison) get pure-logic tests (no filesystem).
- CI runs `go test ./...` on Windows + macOS + Linux — all green is required before merge.
- Existing tests: SDK fetchers in `internal/sdk/*_test.go`; shim system in `internal/shim/shim_test.go` + `internal/shimmanager/manager_test.go`.

## Commit flow (per dev cycle)

1. **Create a fresh `dev` from `main` each cycle** — do NOT reuse an old `dev`. On the remote, create `dev` from `main` (GitHub UI "New branch: dev from main", or `git push origin origin/main:refs/heads/dev`), then `git fetch && git checkout dev`. See "Why recreate dev" below.
2. Commit on `dev` with conventional-commit messages: ASCII, subject ≤100, total message ≤200, no forbidden tokens (see `hook-rules.conf`). Author/committer email must be in `[identities]`. **Include unit tests for new/changed code** (see Testing below) — a PR without tests for its code changes won't be merged.
3. `git push origin dev` (fast-forward). The pre-push hook scans the push range and runs the per-language `test` stage.
4. Open PR `dev → main`. Wait for CI fully green (lint gofmt/vet, test × 3 OSes, commit-lint).
5. **Squash-merge** to `main`. This creates a `noreply@github.com`-committer squash commit on `main` — fine, `main` is never pushed by you.
6. **Delete `dev` via the GitHub UI "Delete branch"** (after the merge). Do not keep accumulating across cycles on one `dev`.

## Why recreate `dev` each cycle (never rebase/merge dev onto main)

`main`'s squash-merge commits have `committer = GitHub <noreply@github.com>`. The pre-push identity scan rejects any noreply-committer commit inside `dev`'s push range, so:

- `dev` can never be rebased onto or merged with `main` — the noreply squash commits would enter `dev`'s push range and be rejected.
- Reusing one `dev` across cycles lets its merge-base go stale, causing add/add conflicts on files a previous PR added.

Creating `dev` fresh from `main` each cycle keeps `dev`'s push range = only the new commits (main's noreply squashes already sit on `origin/main`, so they're not counted as "new" and are not scanned), so pre-push passes and the merge-base stays current.

## Caveats

- `git push --delete <branch>` and force-pushes are rejected by the pre-push hook (it errors on `new=0000` ref deletions and on noreply in a force-push range). Use the GitHub UI "Delete branch" instead. If a CLI delete/force is unavoidable, it requires `--no-verify`.
- Same applies to deleting a tag: use the GitHub UI, not `git push --delete <tag>`.
