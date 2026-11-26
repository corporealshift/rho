# rho

Go coding-agent harness: the daemon owns append-only session logs; policy lives in modules.

- Gate: `go build ./... && go vet ./... && go test ./...`, and `gofmt -l .` prints nothing. Run all of it, not one package.
- The invariants are in ARCHITECTURE.md. Decisions are in `docs/specs/2026-09-11-rho-architecture-design.md` (60 KB): grep for the section. Never read `docs/plans/*` whole (up to 319 KB).
- Changing an event or method means changing `protocol/spec.md`, the JSON schema and a conformance vector together.
- Modules import only `daemon/module` and `protocol`. A new module goes in `daemon/modules/<name>/` plus one line in `all.go`.
- Commits: `area: lowercase summary`. Stage named files, never `git add -A`.
