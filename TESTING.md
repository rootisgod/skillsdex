# Testing Methodology

SkillsDex uses behavior-first tests. New work should follow a red-green-refactor loop: add one failing test for one observable behavior, write the smallest implementation that passes it, then refactor only after the suite is green.

## Standard

- Test through public surfaces: CLI commands, HTTP routes, exported API responses, SQLite-backed domain functions, manifest files, and installed skill files.
- Mock only true boundaries: GitHub, agentskill.sh, stdin/stdout, time, and the filesystem where a real temporary directory would be too broad.
- Prefer a real temporary SQLite database over database mocks.
- Name tests after behavior the user or caller cares about.
- Prefer table-driven subtests for parsers, validators, and matrix-style behavior.
- Avoid assertions that only prove internal call order or request count when a user-visible result proves the behavior.
- Keep one logical behavior per test or subtest.

## Tracer Bullet Workflow

For a feature or bug fix:

1. Confirm the public interface and the most important behavior.
2. Add one failing test for the smallest end-to-end slice.
3. Implement only enough code to make that test pass.
4. Repeat with the next behavior.
5. Refactor duplicated setup or shallow helpers after tests pass.

## Critical Coverage Areas

Coverage gates focus on behavior-risk areas, not a blanket percentage:

- Auth, API token, session, and CSRF behavior.
- Import and source refresh behavior.
- Backup export, local inspection, dry-run restore, and restore upload behavior.
- Install, sync, uninstall, overwrite, and modified-file protection.
- Full-folder skill package handling.
- CLI JSON output and scriptability.

## Required Verification

Before merging a non-trivial change, run:

```bash
task verify
```

That wraps:

```bash
go test ./...
go vet ./...
staticcheck ./...
go test -race ./...
task coverage-critical
task e2e-first-run
```

If `golangci-lint` is available in the environment, also run:

```bash
golangci-lint run
```
