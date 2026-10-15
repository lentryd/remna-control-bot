# Project rules

## Checks before committing and pushing

CI (`.github/workflows/test.yaml`) fails on any of the checks below, so run them locally before every `git commit` that changes code and before every `git push`, and fix whatever fails. Don't push until they are all green:

- `gofmt -l $(git ls-files '*.go')`: the output must be empty
- `go vet ./...`
- `go test ./...`
- `golangci-lint run`: run it **separately from the tests**; CI catches things on it that `go test` misses (for example unchecked errors in tests, errcheck)
- if `web/` changed: `cd web && bun run typecheck && bun run build`

In short: `task lint` and `task test` from `taskfile.yaml` (plus `bun run build` for the frontend).

After a push, check the CI result with `gh run list --limit 3`; if `Test` failed, look at `gh run view <id> --log-failed` and fix it right away.
