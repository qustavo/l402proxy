# AGENTS.md — Coding Guidelines for l402proxy

This file provides coding standards and commands for agents working on the l402proxy codebase.

---

## Build/Test/Lint Commands

### Build
```bash
# Build the main binary
go build -o l402proxy ./cmd/l402proxy

# Install locally
go install ./cmd/l402proxy
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with verbose output
go test -v ./...

# Run a single test
go test -v ./pkg/macaroon -run TestIssueAndVerify

# Run a single test in a specific package
go test -v ./pkg/proxy -run TestServeHTTP_NoAuth_Returns402

# Run all tests in a package
go test -v ./pkg/macaroon
go test -v ./pkg/proxy
go test -v ./pkg/lightning

# Run tests with race detection
go test -race ./...
```

### Linting
```bash
# Run go vet (built-in static analyzer)
go vet ./...

# Run go fmt check (returns non-zero if files need formatting)
test -z "$(gofmt -l .)"

# Format all Go files
go fmt ./...
```

---

## Code Style Guidelines

### General Principles
- **Idiomatic Go**: Follow standard Go conventions and effective Go practices
- **Simplicity first**: Prefer clarity over cleverness
- **Zero dependencies**: Minimize external dependencies beyond core requirements
  (urfave/cli, grpc, lnd/lnrpc). Use stdlib wherever possible.
- **Production quality**: This is not prototype code — write it as if deploying to production

### Imports
- **Stdlib first, then external, then internal** (separated by blank lines):
  ```go
  import (
      "context"
      "fmt"
      "time"

      "github.com/urfave/cli/v2"
      "google.golang.org/grpc"

      "github.com/qustavo/l402proxy/pkg/lightning"
  )
  ```
- Use **goimports** ordering (stdlib, external, local)
- No dot imports, no blank imports except for side-effects (rare)

### Formatting
- **Use `gofmt`** — non-negotiable. All code must be gofmt'd.
- **Line length**: No hard limit, but prefer <100 chars for readability
- **Tabs for indentation** (Go standard)

### Types & Interfaces
- **Keep interfaces small**: Follow the "accept interfaces, return structs" principle
- **Pointer receivers** for methods that modify state or are on large structs
- **Value receivers** for methods that don't modify state on small types
- **Exported types** start with capital letters, unexported with lowercase
- **Use named return values** sparingly — only when they improve clarity

### Naming Conventions
- **Package names**: lowercase, single word, no underscores (e.g., `macaroon`, `lightning`, `proxy`)
- **Variables**: camelCase (unexported), PascalCase (exported)
- **Constants**: PascalCase (not SCREAMING_SNAKE_CASE, unless interfacing with C)
- **Avoid stutter**: Don't repeat package name in type names (e.g., `macaroon.Service`, not `macaroon.MacaroonService`)
- **Short variable names** in small scopes: `ctx`, `err`, `req`, `resp`, `cfg`, `h`, `t`, `s`
- **Longer names** for broader scopes or when clarity demands it

### Error Handling
- **Always check errors**: No `_ = foo()` unless there's a documented reason
- **Wrap errors with context** using `fmt.Errorf("context: %w", err)`
- **Return early**: Avoid deeply nested error checks
  ```go
  // Good
  if err != nil {
      return fmt.Errorf("doing thing: %w", err)
  }
  
  // Bad
  if err == nil {
      // ... lots of nested code
  } else {
      return err
  }
  ```
- **Use `errors.New` for simple errors**, `fmt.Errorf` when formatting or wrapping
- **Sentinel errors** should be package-level `var` with `Err` prefix (rare in this codebase)

### Logging
- **Use `log/slog`** (stdlib, Go 1.21+) for all structured logging
- **Log levels**:
  - `Info`: Normal operation events (server start, invoice created)
  - `Warn`: Non-critical issues (invalid auth header, ephemeral secret key)
  - `Error`: Errors that prevent operation (LND connection failure)
- **Use structured fields**: `slog.String("key", value)`, not string concatenation
- **Keep log messages lowercase** with no trailing punctuation for consistency

### Testing
- **Table-driven tests** for multiple cases of the same logic (see `pkg/macaroon/token_test.go`)
- **Test file naming**: `*_test.go` in the same package
- **Test function naming**: `TestFunctionName_Scenario` (e.g., `TestVerify_Expired`)
- **Use `t.Helper()`** in test helper functions
- **Avoid sleeps**: Use channels or mocks to synchronize instead of `time.Sleep`
- **Mock external dependencies**: See `mockBackend` in `pkg/proxy/handler_test.go`
- **Test error paths**, not just happy paths

### Comments & Documentation
- **Exported types/functions MUST have doc comments** starting with the name:
  ```go
  // Service issues and verifies stateless HMAC-signed tokens.
  type Service struct { ... }
  ```
- **Unexported code** should have comments when non-obvious
- **Package-level doc**: Add a `doc.go` if the package needs explanation (not required yet)
- **TODO comments**: Use `// TODO(username): description` if you must, but prefer fixing immediately

### Crypto & Security
- **Use only stdlib crypto**: `crypto/hmac`, `crypto/sha256`, `crypto/rand`
- **No external crypto libraries** unless absolutely necessary and vetted
- **Constant-time comparisons**: Use `hmac.Equal` for comparing MACs, not `==`
- **Random data**: Use `crypto/rand.Read`, never `math/rand` for secrets

### Go Version
- **Target Go 1.24+** (current version in go.mod: 1.24.4)
- Use modern stdlib features (`log/slog`, `strings.Cut`, `errors.Is/As`)

---

## Architecture Notes

### Package Layout
```
pkg/lightning/  ← Lightning node backends (interface + LND implementation)
pkg/macaroon/   ← Stateless token issuance & HMAC verification
pkg/proxy/      ← HTTP handler implementing L402 flow
cmd/l402proxy/  ← CLI entrypoint
```

### Key Abstractions
- **`lightning.Backend`**: Pluggable Lightning node interface (currently only LND)
- **`macaroon.Service`**: Stateless token service (no database, HMAC-signed)
- **`proxy.Handler`**: HTTP handler implementing the L402 challenge-response flow

### Design Principles
- **Stateless**: No database, tokens are cryptographically signed
- **Single binary**: Everything compiles into one executable
- **Pluggable backends**: Lightning backends implement a simple interface
- **Minimal dependencies**: Stdlib-first approach

---

## Common Patterns

### Context Propagation
Always pass `context.Context` as the first parameter:
```go
func (b *Backend) CreateInvoice(ctx context.Context, ...) (*Invoice, error)
```

### Pointer vs Value Receivers
```go
// Pointer receiver (modifies state or large struct)
func (s *Service) Issue(...) (string, error)

// Value receiver (no mutation, small struct)
func (t Token) IsExpired() bool  // hypothetical
```

### Error Wrapping
```go
if err != nil {
    return fmt.Errorf("creating invoice: %w", err)
}
```

---

## What NOT to Do
- ❌ Don't add database dependencies
- ❌ Don't add heavyweight frameworks
- ❌ Don't use `panic` except in truly unrecoverable situations (rare)
- ❌ Don't ignore errors with `_` without a comment explaining why
- ❌ Don't use `interface{}` when a concrete type suffices (use `any` if you must)
- ❌ Don't create init() functions unless absolutely necessary (side effects are bad)
- ❌ Don't commit commented-out code

---

## References
- Effective Go: https://go.dev/doc/effective_go
- Go Code Review Comments: https://go.dev/wiki/CodeReviewComments
- L402 Protocol: https://github.com/lightninglabs/L402
