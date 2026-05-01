# Contributing to netra-browser

Issues and PRs welcome. Areas where help is most useful:

- New examples in `examples/`
- Cross-platform launch testing (macOS, Windows)
- HTTP-SSE streaming notifications (deferred from v1)
- Companion Chrome extension (planned for v2 in a separate repo)

Look for issues labeled `good-first-issue`.

## Development

- Go 1.22+ required
- Run `go test ./...` for unit tests
- Run `go test -tags e2e ./e2e/...` for end-to-end tests (requires `chromium` in PATH)

## Style

Follow `gofmt -l . | grep -v '^docs/'` (must be empty). Add tests for new behavior.
