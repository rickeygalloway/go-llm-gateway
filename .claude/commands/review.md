Review the code changes or file specified: $ARGUMENTS

If no argument is given, review all staged and unstaged changes (`git diff HEAD`).

## What to check

### Correctness
- Logic errors, off-by-one errors, unreachable code
- Race conditions on shared state (maps, slices, custom types)

### Go conventions
- All errors checked — no `_` discarding errors silently
- Errors wrapped with context: `fmt.Errorf("doing X: %w", err)`
- No `log.Print` / `fmt.Print` in library code — use the structured logger (zerolog)
- No hardcoded config values — all config via Viper
- Goroutines have a clear owner and exit path; no goroutine leaks

### Code quality
- No unused imports or variables
- Exported types and functions have doc comments
- No overly deep nesting — suggest early returns where appropriate

### Tests
- New logic has unit tests
- HTTP dependencies mocked with `httptest` — no real API calls in unit tests
- Integration tests tagged `//go:build integration`

### Security
- No credentials, tokens, or keys in code or comments
- External input validated before use
- No `exec.Command` on user-controlled input

## Output format

For each issue found:
- **file:line** — severity (`critical` / `warning` / `suggestion`)
- One sentence describing the problem
- A suggested fix (inline code if short, otherwise a diff block)

End with a **summary line**: `X critical · Y warnings · Z suggestions`
