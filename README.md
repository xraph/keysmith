# Keysmith — Composable API key management for the Forge ecosystem

[![Go Reference](https://pkg.go.dev/badge/github.com/xraph/keysmith.svg)](https://pkg.go.dev/github.com/xraph/keysmith)
[![Go Version](https://img.shields.io/badge/go-1.24+-blue)](https://go.dev)
[![CI](https://github.com/xraph/keysmith/actions/workflows/ci.yml/badge.svg)](https://github.com/xraph/keysmith/actions/workflows/ci.yml)

Keysmith handles the full lifecycle of API keys: generation, hashing, storage, validation, rotation, revocation, and usage analytics. Raw keys are never persisted — only SHA-256 hashes are stored.

## Features

- **Secure by default** — Raw API keys are returned exactly once at creation; only hashes are stored
- **Key lifecycle** — Create, validate, rotate, revoke, suspend, and reactivate API keys
- **Scoped permissions** — Assign hierarchical permission scopes to keys
- **Policy engine** — Rate limits, IP/origin allowlists, key lifetime constraints, and quotas
- **Usage analytics** — Per-request usage recording with daily/monthly aggregation
- **Rotation with grace periods** — Zero-downtime key rotation with configurable grace windows
- **Plugin system** — Opt-in lifecycle hooks for audit trails, metrics, and authorization sync
- **Forge integration** — Mounts as a `forge.Extension` with DI, REST API routes, and auto-migration
- **Multi-tenant** — Automatic tenant scoping via Forge scope or standalone context helpers

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/xraph/keysmith"
    "github.com/xraph/keysmith/key"
    "github.com/xraph/keysmith/store/memory"
)

func main() {
    // Create an engine with an in-memory store.
    eng, err := keysmith.NewEngine(keysmith.WithStore(memory.New()))
    if err != nil {
        log.Fatal(err)
    }

    // Set tenant context (standalone mode).
    ctx := keysmith.WithTenant(context.Background(), "my-app", "tenant-1")

    // Create an API key — the raw key is returned only once.
    result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
        Name:        "Production Key",
        Prefix:      "sk",
        Environment: key.EnvLive,
        Scopes:      []string{"read:users", "write:users"},
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("API Key:", result.RawKey)

    // Validate the key.
    vr, err := eng.ValidateKey(ctx, result.RawKey)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Validated for tenant:", vr.Key.TenantID)
    fmt.Println("Scopes:", vr.Scopes)
}
```

## Installation

```bash
go get github.com/xraph/keysmith
```

## Architecture

```
keysmith.Engine
├── key.Store          — CRUD + hash-based lookup, state transitions
├── policy.Store       — rate limits, allowed scopes/IPs/origins
├── scope.Store        — hierarchical permission scopes, key-scope junction
├── usage.Store        — per-request records, daily/monthly aggregation
├── rotation.Store     — rotation history, grace period tracking
├── plugin.Manager     — lifecycle hook dispatch (audit, metrics, warden)
├── Hasher             — SHA-256 key hashing with constant-time verification
├── KeyGenerator       — {prefix}_{env}_{64 hex} format
└── RateLimiter        — pluggable rate limiting interface
```

### Key States

```
active → rotated → revoked
active → suspended → active (reactivate)
active → expired
active → revoked
```

## Packages

| Package | Description |
|---------|-------------|
| `keysmith` | Core engine, types, hasher, generator |
| `key` | Key entity, lifecycle states, store interface |
| `policy` | Policy entity, store interface |
| `scope` | Scope entity, key-scope assignment, store interface |
| `usage` | Usage records, aggregation, store interface |
| `rotation` | Rotation records, reasons, store interface |
| `store` | Composite store interface embedding all sub-stores |
| `store/memory` | In-memory store for testing |
| `store/postgres` | PostgreSQL store with embedded migrations |
| `plugin` | Lifecycle hook interfaces and dispatch manager |
| `audit_hook` | Audit trail plugin (emits structured events) |
| `observability` | Metrics plugin (go-utils counters) |
| `warden_hook` | Warden authorization bridge plugin |
| `api` | Forge-style REST API handlers with OpenAPI metadata |
| `middleware` | HTTP middleware for API key validation and scope checks |
| `extension` | Forge extension adapter (DI, routes, migration) |
| `id` | TypeID-based entity identifiers (akey, kpol, kusg, krot, kscp) |

## Plugins

Keysmith uses an opt-in plugin system. Plugins implement `plugin.Plugin` (requires only `Name()`) and any combination of lifecycle hooks:

```go
eng, _ := keysmith.NewEngine(
    keysmith.WithStore(store),
    keysmith.WithExtension(audithook.New(myRecorder)),
    keysmith.WithExtension(observability.NewMetricsExtension()),
)
```

### Available Hooks

| Hook | Fired when |
|------|-----------|
| `KeyCreated` | Key successfully created |
| `KeyCreateFailed` | Key creation fails |
| `KeyValidated` | Key passes validation |
| `KeyValidationFailed` | Key validation fails |
| `KeyRotated` | Key is rotated |
| `KeyRevoked` | Key is permanently revoked |
| `KeySuspended` | Key is temporarily suspended |
| `KeyReactivated` | Suspended key is reactivated |
| `KeyExpired` | Key found expired during validation |
| `KeyRateLimited` | Key exceeds rate limit |
| `PolicyCreated` | Policy created |
| `PolicyUpdated` | Policy updated |
| `PolicyDeleted` | Policy deleted |
| `Shutdown` | Engine shutting down |

## Forge Extension

Mount Keysmith as a Forge extension for automatic DI registration, REST API routes, and migration:

```go
app := forge.New(
    forge.WithExtension(
        extension.New(
            extension.WithEngineOptions(
                keysmith.WithStore(pgStore),
            ),
            extension.WithHookExtension(audithook.New(recorder)),
        ),
    ),
)
```

## REST API

When mounted via the Forge extension, Keysmith exposes these endpoints:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/keys` | Create API key |
| `GET` | `/v1/keys` | List API keys |
| `GET` | `/v1/keys/:keyId` | Get API key |
| `DELETE` | `/v1/keys/:keyId` | Delete API key |
| `POST` | `/v1/keys/:keyId/rotate` | Rotate API key |
| `POST` | `/v1/keys/:keyId/revoke` | Revoke API key |
| `POST` | `/v1/keys/:keyId/suspend` | Suspend API key |
| `POST` | `/v1/keys/:keyId/reactivate` | Reactivate API key |
| `POST` | `/v1/keys/validate` | Validate raw API key |
| `POST` | `/v1/policies` | Create policy |
| `GET` | `/v1/policies` | List policies |
| `GET` | `/v1/policies/:policyId` | Get policy |
| `PUT` | `/v1/policies/:policyId` | Update policy |
| `DELETE` | `/v1/policies/:policyId` | Delete policy |
| `POST` | `/v1/scopes` | Create scope |
| `GET` | `/v1/scopes` | List scopes |
| `DELETE` | `/v1/scopes/:scopeId` | Delete scope |
| `POST` | `/v1/keys/:keyId/scopes` | Assign scopes to key |
| `DELETE` | `/v1/keys/:keyId/scopes` | Remove scopes from key |
| `GET` | `/v1/keys/:keyId/usage` | Get key usage |
| `GET` | `/v1/keys/:keyId/usage/aggregate` | Get usage aggregation |
| `GET` | `/v1/usage` | List tenant usage |
| `GET` | `/v1/keys/:keyId/rotations` | List key rotations |

## License

Part of the Forge ecosystem.
