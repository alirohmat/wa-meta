# Repository Guidelines

## Project Structure & Module Organization
- `main.go` — single-entry service (1679 LOC): WhatsApp pairing (`whatsmeow`), BOT IN/OUT bridge, SSE live log, CAS media download, `state`/`telemetry` SQLite tables.
- `go.mod` / `go.sum` — module `wabot` (Go 1.26), key deps `go.mau.fi/whatsmeow`, `mattn/go-sqlite3`.
- Runtime artifacts (gitignored): `./media/` (sharded CAS `ab/cd/<sha256>.<ext>`), `*.db`/`store.db` SQLite store. Do not commit.
- No subpackages yet; add `internal/` only when `main.go` extraction exceeds one responsibility.

## Build, Test, and Development Commands
```bash
go run .                          # start server on :8000, prints BOT JID
PORT=8000 BOT=867051314767696@bot MEDIA_DIR=./media go run .
go build -o wabot .               # production binary
go vet ./... && go fmt ./...      # vet + format (required before PR)
go mod tidy                       # sync deps after editing go.mod
```
Web UI: `http://localhost:8000` — pairing code `XXXX-XXXX`, SSE `/events`, logs `/api/logs`, media `/api/media`.

## Coding Style & Naming Conventions
- `gofmt` canonical formatting; `tabs` for indentation; run `go fmt` before commit.
- Exported names `CamelCase`, unexported `camelCase`; receivers short (`b *bridge`, `h *hub`).
- Env helpers via `envStr(k, def)`; new config must follow `os.Getenv` + default pattern.
- Keep `main.go` flat until split justified; mark deliberate shortcuts with `// ponytail: <ceiling> -> <upgrade>`.

## Testing Guidelines
- No test suite yet; use `go test ./...` when added.
- Place tests beside code as `*_test.go`; use stdlib `testing` + `assert` helpers, no new framework unless justified.
- For bridge/media changes, add focused table-driven test covering CAS path-traversal and `mimeToExt` mappings.
- Verify manually: pairing flow, BOT IN/OUT SSE warp, media `PUBLIC_PREFIX` serving.

## Commit & Pull Request Guidelines
- No git history established; use Conventional Commits: `feat:`, `fix:`, `chore:`, `docs:` — imperative, <72 chars.
- PRs: clear description, linked issue, reproduction steps, screenshot for UI/SSE changes, `go vet`/`go fmt` clean.
- Small, single-purpose commits; delete dead code rather than commenting out.

## Security & Configuration Tips
- Never commit `*.db`, `./media/*`, or tokens. Configure via env: `BOT`, `WEBHOOK`, `MEDIA_DIR`, `PUBLIC_PREFIX`, `PORT`.
- Validate all `filepath.Join(mediaDir, ...)` with `filepath.Abs` + prefix check (existing `saveBytesToCAS` guard).
- Rate-limit and webhook hooks are in-memory; do not log raw E2E payloads.

## Architecture Overview
HTTP mux + `whatsmeow` client + `sqlstore` → `bridge` channels (`mediaJobs`, `stateWrite`, `telemetry`). SSE `hub` broadcasts bot logs; media workers fetch CDN/WA bytes → CAS disk → `media_ready` webhook.
