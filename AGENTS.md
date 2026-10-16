# Keysmith

Keysmith is an API key engine for Go. You create, validate, rotate, suspend and revoke keys, attach policies (rate limits, quotas, allowed scopes) and permission scopes to them, and record usage, all tenant-scoped. The store keeps each key's hash and a short hint, and you only see the raw key in what create and rotate return. It's a library with a Forge extension, HTTP handlers and a validation middleware.

Module path: `github.com/xraph/keysmith`. It's a single module with its `go.mod` at the root. `_examples/` is inside that module, but Go skips directories whose names start with an underscore, so `./...` never builds it.

## Fake keys in tests

Keysmith's default key format is `<prefix>_<env>_<random>`. Put the `sk` prefix and the `live` environment in front of 24 or more alphanumerics and you've written something that matches GitHub's Stripe secret-key pattern. GitHub push protection rejects the whole push over it, even though the key is fake.

A test never holds that literal in one piece. You build it at runtime from parts, the way `engine_rotation_test.go` does:

```go
legacyRaw := "sk_" + "live_" + "legacyrawkeybeforethefix0001"
```

Or skip the literal and let the engine make a real one: `mustCreate(t, eng, ctx, nil)` in `engine_state_test.go` returns a `*key.CreateResult` whose `RawKey` you can validate. The short literals in `hasher_test.go` are under the length that matches, but don't lean on counting characters. The same goes for docs, comments and this file: you'll only ever see the prefix here, never a full key.

## Commands

```sh
GOWORK=off go test ./...                  # memory and sqlite only
make test-backends                        # adds postgres and mongo in Docker
GOWORK=off go test -race -count=1 ./...   # what release.yml runs
GOWORK=off go vet ./...
golangci-lint run ./...                   # v2, configured by .golangci.yml
gofmt -s -w . && goimports -w -local github.com/xraph/keysmith .
```

You also get `make test` (`go test -v ./...`), `make test-race`, `make vet`, `make lint`, `make lint-fix`, `make fmt`, `make check` and `make coverage`. `make build`, `make run`, `make install`, `make dev` and `make all` point at `./cmd/keysmith`, which doesn't exist, so they fail.

CI is `.github/workflows/ci.yml`, which calls the shared `xraph/workflows` `go-ci.yml@v1`. It runs `make test`, a race-enabled coverage pass, golangci-lint at the version that workflow pins, `gofmt -l .`, `make vet`, gosec, govulncheck, and a tidiness check that runs `go mod tidy` and fails if `go.mod` or `go.sum` changed. Gosec and govulncheck findings fail the build. `ci.yml` adds a Format job (`goimports -l -local github.com/xraph/keysmith .`) and a Docs job that runs `pnpm install --frozen-lockfile`, `pnpm types:check`, `pnpm lint` and `pnpm build` in `docs/`.

## Tests against Postgres and Mongo

Most store-touching tests go through `storetest.Each` in `internal/storetest`. It always runs memory and sqlite. It adds a postgres subtest only when `KEYSMITH_TEST_PG_DSN` is set, and a mongo subtest only when `KEYSMITH_TEST_MONGO_URI` is set. Leave the variables unset and those subtests are never created at all, so `go test -v` won't even print a SKIP line to tell you they're missing. The migration tests in `store/postgres` and `store/mongo` read the same variables and do call `t.Skip`. CI sets neither. A green CI run has tested memory and sqlite, and that's all.

`make test-backends` runs `scripts/test-backends.sh`. That script starts Postgres and Mongo containers, waits for both to answer a real query, exports the two variables, runs `go test -count=1 ./...` and removes the containers on exit. The host ports default to 34437 and 34717, below 49152 because macOS hands out the range above that to outbound connections. If either port is taken, set `KEYSMITH_TEST_PG_PORT` or `KEYSMITH_TEST_MONGO_PORT`. Extra arguments go straight to `go test`, so `./scripts/test-backends.sh -run TestRotate` works.

Each postgres and mongo subtest gets a database of its own, dropped again when the subtest ends. SQLite opens a file under `t.TempDir()` with the DSN from `sqlite.DSN`. When you write a test that touches a store, wrap it in `storetest.Each` and it runs on every backend you have available.

## Layout

| Path | What lives there |
|---|---|
| root (`package keysmith`) | `Engine` (`engine.go`), `NewEngine` and its options, `Hasher`, `KeyGenerator`, `RateLimiter`, sentinel errors, spans and metrics (`telemetry.go`) |
| `key/`, `policy/`, `scope/`, `usage/`, `rotation/` | One subsystem each, with its entities and its own `Store` interface |
| `store/` | `store.Store`, plus the not-found and conflict sentinels every backend returns |
| `store/memory`, `store/sqlite`, `store/postgres`, `store/mongo` | The four backends. `sqlite.DSN` builds a DSN with the busy timeout and foreign keys set |
| `internal/storetest` | `Each`, the per-backend runner, plus the conformance tests |
| `internal/groveset` | Builds an UPDATE's SET list by hand when one column needs an expression (`version = version + 1`) |
| `plugin/` | Lifecycle hook interfaces (`KeyCreated`, `KeyRotated`, `KeyRevoked` and the rest) |
| `audit_hook/`, `warden_hook/` | Bridges to an audit trail and to Warden. Each declares a local interface (`Recorder`, `WardenBridge`) so keysmith imports neither chronicle nor warden |
| `api/`, `middleware/`, `observability/` | Admin HTTP handlers, request-time key validation for Forge, lifecycle metrics |
| `extension/` | The Forge extension. `extension/contract` wires the dashboard contract (`manifest.yaml`) |
| `id/` | TypeID identifiers |
| `scripts/` | `test-backends.sh` |
| `docs/` | Fumadocs site (Next.js, pnpm). `MIGRATION.md` covers the move to the React dashboard |

## Conventions

Sentinels are prefixed `keysmith: `. You'll find the validation and state errors in the root `errors.go`. The not-found sentinels and `ErrKeyConflict` live in `store/errors.go`, and the root re-exports them under the same names, so `errors.Is` works with either. Backends map a miss through `store.NotFound(entity)` (each has an `errNotFound` helper that calls it) and wrap everything else with a `keysmith/<backend>: <op>: %w` prefix, such as `keysmith/postgres:`.

IDs come from `id/`, with one TypeID prefix per entity (`akey`, `kpol`, `kusg`, `krot`, `kscp`). You make them with `id.NewKeyID()`, `id.NewPolicyID()` and their siblings.

`store.Store` is a set of accessors (`Keys()`, `Policies()`, `Usages()`, `Rotations()`, `Scopes()`) plus `Migrate`, `Ping` and `Close`, and every backend asserts `var _ store.Store = (*Store)(nil)`. If you add a store method, it goes into the subsystem's `Store` interface and then into all four backends, or the build breaks. Key writes are optimistic: `key.Store.UpdateIfVersion` writes only when the stored version matches the one you read and returns `store.ErrKeyConflict` otherwise. Migrations are a grove `migrate.NewGroup("keysmith")` in each backend's `migrations.go`. The `.sql` files under `store/postgres/migrations/` aren't loaded by any Go code. Change `migrations.go` instead.

Options are functional: `type Option func(*Engine)` for the engine, `type ExtOption func(*Extension)` for the extension, with `WithEngineOptions` passing engine options through. Per-call options exist too, such as `RotateOption` and `WithGrace` on `RotateKey`.

The Forge extension is `extension.New(opts...)`, embedding `*forge.BaseExtension` and asserting `var _ forge.Extension = (*Extension)(nil)`. `Register` loads config from `extensions.keysmith` (falling back to the legacy `keysmith` key), resolves a `*grove.DB` from the container (named under `WithGroveDatabase`), builds the matching store and publishes `*keysmith.Engine` with `vessel.Provide`. It warns when a sqlite database has no busy timeout. Routes mount under `/keysmith` unless you set a base path. `Start` runs `Migrate` (unless you turned that off) and starts the engine. Authsome takes the engine through `authsome.WithKeysmith`.

Logging uses `github.com/xraph/go-utils/log`. If you don't pass a logger you get `log.NewNoopLogger()`. Fields are typed, like `log.String("key_id", ...)` and `log.Any("error", err)`, and inside the extension you log with `forge.F` fields.

Tests use testify (`require` and `assert`) on top of `testing`.

### Telemetry

Span names, attribute keys, metric names and every label value live as constants in `telemetry.go`, and `docs/content/docs/subsystems/observability.mdx` lists them all. Change one and you change the other. Spans come from `startSpan` (the tracer `github.com/xraph/keysmith`, looked up per call). Metrics are declared once in `newTelemetry` with `forge.CounterVec` and friends on the sink from `WithMetrics`, and a nil `*telemetry` records nothing, so every recording method checks for nil.

Nothing that identifies a key or a person goes on a span or a label: not the raw key, its hash, hint or ID, not a tenant, app, owner or anything a caller typed. A failure goes on a span through `markError`, which records the error's Go type and never its text. `TestTelemetryCarriesNoPersonalDataOrKeyMaterial` feeds an email address through every path; extend it when you add a span. Refusals (invalid key, refused transition, not found, conflict) and a cancelled caller (`context.Canceled`, result `cancelled`) are results, not errors, and leave the span status alone. A deadline is still an error.

### Lint rules that bite

- `govet` runs with `enable-all` (minus `fieldalignment`), so `shadow` is on, and the gocritic `diagnostic` tag adds `sloppyReassign`. An inner `err :=` that shadows an outer one fails, and so does `if err = f(); err != nil`. Name the inner error something else.
- `errcheck` has `check-type-assertions` on, so use the two-value form. Unlike chronicle and relay, `check-blank` is off here: `_ = f()` passes.
- gosec excludes G104, G304 and G401 in `.golangci.yml`, but CI's standalone gosec job excludes only G115, and it doesn't read `//nolint`. A real false positive needs `// #nosec Gxxx` followed by the reason.
- `goimports` groups `github.com/xraph/keysmith/...` imports last. `revive` checks receiver names, lowercase error strings and unused parameters (`_`). `nolintlint` reports a `//nolint` that suppresses nothing, and the code always gives one a reason after `//`.
- `_test.go` files are exempt from gosec, errcheck and gocritic, and `_examples/` from errcheck and gosec.

## Dependencies

Keysmith depends on grove (`grove`, `drivers/mongodriver`, `drivers/pgdriver`, `drivers/sqlitedriver`) and forge, plus `go-utils` and `vessel`. Authsome consumes it. Versions are in `go.mod`.

Grove and forge tag every submodule with the same version, so bump all of a repo's lines at once:

```sh
GOWORK=off go get github.com/xraph/grove@vX.Y.Z github.com/xraph/grove/drivers/pgdriver@vX.Y.Z  # and every other grove line
GOWORK=off go get github.com/xraph/forge@vA.B.C
GOWORK=off go mod tidy
GOWORK=off go test ./...
```

It's a single module, so plain `go mod tidy` covers it. Keep `GOWORK=off` on your Go commands so a `go.work` somewhere above the checkout can't change what resolves. CI has none. Never commit a `go.work`, and never commit a `replace` pointing at a sibling checkout. It builds on your machine and breaks `go get` for everyone else.

## Releasing

You release by dispatching the workflow. Never push a tag by hand.

```sh
gh run list --workflow ci.yml --branch main --limit 1   # wait for green
gh workflow run release.yml --ref main -f tag=vX.Y.Z
```

`release.yml` tags the dispatched commit and pushes the tag first. After that it runs `go mod download`, `go build ./...` and `go test -race -count=1 ./...`, and publishes the GitHub release (notes from `git log` since the previous tag) only if they pass. A failure at that point leaves a public tag with no release, so fix forward with the next patch version.

Release keysmith after the grove and forge versions it needs are tagged and pinned here. Authsome re-pins afterwards.

Commit subjects use conventional prefixes: `fix(store):`, `feat(contract):`, `test:`, `docs:`, `chore:`.

## Branch rules

`main` has no active rules. A ruleset named `main` exists, but its enforcement is disabled, so direct pushes and force-pushes both go through and nothing is required to pass first.
