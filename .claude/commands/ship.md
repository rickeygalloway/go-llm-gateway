Run all quality checks and report results before committing or opening a PR.

## Steps

Run `make check` which executes: fmt → vet → lint → test.

If `make check` is not available, run each step manually in order. Stop and report on first failure.

1. **Format**: `make fmt` (or `gofmt -s -w . && goimports -w .`)
2. **Vet**: `make vet` (or `go vet ./...`)
3. **Lint**: `make lint` (or `golangci-lint run --timeout=5m`)
4. **Test**: `make test` (or `go test ./... -race -count=1 -timeout=60s`)

## Output format

For each step, print one of:
- `✓ fmt` — passed
- `✗ fmt — N issues` — failed, followed by the raw output

If all steps pass, end with:
> All checks passed. Safe to commit.

If any step fails, end with:
> Stopped at <step>. Fix the issues above before committing.
