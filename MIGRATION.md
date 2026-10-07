# Migrating to the React dashboard

## What changed

Keysmith's dashboard is now the React package `@forge-go/dashboard-plugin-keysmith`, which lives in the forge-dashboard repository (`packages/plugin-keysmith`) and talks to keysmith through the contract contributor the extension registers in `Extension.RegisterContractContributor`. The Go-rendered templ dashboard under `dashboard/` is gone, and so is `Extension.DashboardContributor()`, which c08cf0f removed once forge main dropped the contributor package. To turn the new one on you run forge's dashboard extension with the React shell, and you tell keysmith which tenant the dashboard works in, because no principal carries a tenant claim today and keysmith won't guess: set `extensions.keysmith.dashboard.tenant_id` (and, if you want new rows labelled, `app_id`) in YAML, or pass `WithDashboardTenant(tenantID, appID)` when you build the extension, and YAML wins when both are set. Leave the tenant empty and every dashboard request answers `PERMISSION_DENIED` with a message naming that config key. Every page shows one tenant. The templ pages set no tenant at all, so they showed you every tenant's rows, and rows written with an empty tenant (standalone use, authsome's keysmith adapter) can't be reached from the new dashboard.

```yaml
extensions:
  keysmith:
    dashboard:
      tenant_id: acme
      app_id: billing   # optional, only labels rows the dashboard creates
```

```go
ext := extension.New(extension.WithDashboardTenant("acme", "billing"))
```

The new dashboard also writes. You can create keys (the raw key is shown once), rotate them with a grace window you choose, end a window early, suspend, reactivate and revoke, assign and remove scopes, and create, edit and delete policies and scopes. The templ dashboard could only rotate, suspend, reactivate and revoke, through GET links.

## Inventory

Every page, column, action, filter, badge, widget and empty state in the templ dashboard, read from the `.templ` files and from `contributor.go`, `data.go`, `manifest.go` and `plugin_iface.go` before they were deleted. React paths are relative to `packages/plugin-keysmith/src/` in forge-dashboard. Intents are the contract's, from `extension/contract/manifest.yaml`.

Three things changed on every page, so the tables don't repeat them:

- The templ pages ran actions as GET requests (`/keys/detail?key_id=...&action=revoke`) behind a browser `confirm()`. The React pages send contract commands, and every destructive action has its own dialog.
- Templ rows were clickable through htmx. React rows link by name.
- Templ lists read one fixed page (50 keys, 100 policies, 200 scopes, 50 rotations, 50 usage records) with no paging. React lists page, and say when there is more.

### Overview (`/`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Stat: Total Keys | Replaced | `pages/overview.tsx`, `overview` | Active keys, with suspended, revoked and expired counted in the card's hint. Counts use the state the badges show (3928145). |
| Stat: Active Keys | Migrated | `pages/overview.tsx`, `overview` | |
| Stat: Policies | Dropped | | The spec's overview leads with what needs attention: open grace windows, keys expiring within 7 days, requests in the last 24h. The Policies page captions its count. |
| Stat: Scopes | Dropped | | Same reason. The Scopes page captions its count. |
| Recent API Keys table (5): Name, Hint, Environment, State, Created | Migrated | `pages/overview.tsx`, `overview` | Hint and Environment are one Key column (`sk_live_…a3f8`). |
| Empty state "No API keys yet" | Migrated | `pages/overview.tsx` | "No keys yet." |
| Plugin sections slot (`Plugin.DashboardWidgets`) | Dropped | | The plugin interfaces are gone; see Manifest and contributor. |

### Keys (`/keys`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Count badge in the header | Migrated | `pages/keys.tsx`, `keys.list` | The table caption, from the server's total. |
| Filter: environment (All, Live, Test, Staging) | Migrated | `pages/keys.tsx`, `keys.list` `environment` | |
| Filter: state (All, Active, Revoked, Suspended, Expired) | Migrated | `pages/keys.tsx`, `keys.list` `state` | |
| Search box "Search keys by name" | Dropped | | It sent `name`, and nothing read it: `renderKeys` ignored the parameter, so the box never filtered anything. `keys.list` has no name filter. |
| Column: Name | Migrated | `pages/keys.tsx` | Links to the key. |
| Column: Hint (`****a3f8`) | Migrated | `pages/keys.tsx` | Key column, `prefix_env_…hint`, mono. |
| Column: Environment (badge) | Replaced | `pages/keys.tsx` | Plain text, as the spec rules. |
| Column: State (badge) | Migrated | `badges.tsx` `KeyStateBadge` | Badge weights changed; see Shared components. |
| Column: Last Used ("Never") | Migrated | `pages/keys.tsx` | |
| Column: Created | Replaced | `pages/key-detail.tsx` Details | The list is newest first; the date is on the key's page. Policy, Scopes and Expires columns are new. |
| Empty state "No API keys found" | Migrated | `pages/keys.tsx` | "No API keys yet." with Create key, or "No keys match these filters." |

### Key detail (`/keys/detail?key_id=`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Back to Keys | Replaced | | The sidebar's Keys entry. A missing key shows "No key with this id." with Back to keys. |
| Header: name, description, or `prefix_****hint` when there is none | Migrated | `pages/key-detail.tsx`, `keys.detail` | The masked key always shows under the name. |
| Header: state badge | Migrated | `pages/key-detail.tsx` | Also explains an expiry the engine hasn't marked yet. |
| Header: environment badge | Replaced | Details section | Text. |
| Fields: Key ID, Prefix, Created By, Created, Updated, Last Used, Expires, Last Rotated, Revoked At | Migrated | Details section, `keys.detail` | |
| Fields: Hint, Environment, State | Replaced | Header and Details | Hint is in the masked key, State is the badge. |
| Action: Rotate (active keys, `reason=manual`, new key thrown away) | Replaced | `components/rotate-key-dialog.tsx`, `keys.rotate` | Asks for a reason and a grace, shows the new key once, and states the previous key's window. Offered on suspended keys too. The templ action discarded the new raw key, which left the key unusable. |
| Action: Suspend | Migrated | `components/key-actions.tsx`, `keys.suspend` | |
| Action: Revoke (active keys only, fixed reason "Revoked via dashboard") | Migrated | `components/key-actions.tsx`, `keys.revoke` | Asks for a reason and is offered on any key that isn't revoked. The reason goes to the hooks; keysmith doesn't store it, so no page shows it later. |
| Action: Reactivate (with a confirm) | Migrated | `components/key-actions.tsx`, `keys.reactivate` | No confirm: putting a key back is what you suspended it for. |
| Stat: Usage (all-time request count) | Replaced | Usage section, `usage.series` | Requests over the last 7 UTC days, with a compact chart. |
| Stat: Age | Replaced | Details "Created" | |
| Stat: Rotations (count) | Replaced | Rotation history section | Lists the 10 newest and says when there are more. |
| Stat: Last Used | Migrated | Details "Last used" | |
| Policy card: name, description | Migrated | Policy section, `keys.detail` | The name links to the policy page, where the description is. |
| Policy card: max lifetime, grace period | Migrated | Policy section | |
| Policy card: rate limit, burst, daily and monthly quota, rotation period | Replaced | `pages/policy-detail.tsx` | The key page names what Keysmith enforces. The rest sits on the policy page under the group that says who enforces it. |
| Assigned Scopes card, with count badge | Replaced | `components/scopes-editor.tsx`, `keys.scopes.assign`, `keys.scopes.remove` | You can add and remove scopes here now. |
| Empty state "No scopes assigned." | Migrated | Scopes section | |
| Recent Usage table (20 rows: endpoint, method, status, latency, time) | Replaced | `pages/usage.tsx`, `usage.records` | The key page shows the chart. The rows are on Usage with the key picked in its Key filter. |
| Recent Usage: View All (to `/usage/detail?key_id=`) | Replaced | "Open usage" | Lands on Usage unfiltered; see Not surfaced. |
| Empty state "No usage recorded yet." | Migrated | Usage section | "Usage appears once your application calls RecordUsage." |
| Rotation History table: Reason, Grace Period, Grace Ends, Rotated At | Migrated | Rotation history section, `rotations.list` `keyId` | Each row shows the old and new key and whether its window is open. Grace Period is the Grace column on Rotations. |
| Rotation History count badge | Replaced | | "Showing the 10 newest." when there are more. |
| Empty state "No rotations recorded." | Migrated | | "This key has not been rotated." |
| Plugin sections slot (`KeyDetailContributor`) | Dropped | | Nothing in forgery implements it (spec). |

### Policies (`/policies`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Count badge | Migrated | `pages/policies.tsx`, `policies.list` | Table caption. |
| Column: Name, with the description beneath | Migrated | `pages/policies.tsx` | Name links to the policy. The description is the policy page's header. |
| Column: Rate Limit ("Unlimited") | Replaced | `pages/policy-detail.tsx` | Under "Enforced only with a rate limiter", which says whether this deployment has one. |
| Column: Daily Quota, Monthly Quota ("None") | Replaced | `pages/policy-detail.tsx` | Under "Stored for your application": keysmith never checks them. |
| Column: Key Lifetime ("Unlimited") | Migrated | `pages/policies.tsx` | Max lifetime. Grace and Allowed scopes columns are new. |
| Column: Created | Replaced | `pages/policy-detail.tsx` Details | |
| Empty state "No policies defined" | Migrated | | "No policies yet." with Create policy. |

### Policy detail (`/policies/detail?policy_id=`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Back to Policies | Replaced | | The sidebar's Policies entry. A missing policy shows "No policy with this id." with Back to policies. |
| Header: name, description, or the truncated ID | Migrated | `pages/policy-detail.tsx`, `policies.detail` | |
| Fields: Policy ID, Created, Updated | Migrated | Details | |
| Rate Limiting card: Rate Limit, Window | Migrated | "Enforced only with a rate limiter" | One Rate limit row. |
| Rate Limiting card: Burst Limit | Migrated | "Stored for your application" | |
| Quotas card: Daily, Monthly | Migrated | "Stored for your application" | |
| Key Lifecycle card: Max Key Lifetime, Grace Period | Migrated | "Enforced by Keysmith" | Unset grace reads "24 hours (default)", which is what rotation uses. |
| Key Lifecycle card: Rotation Period | Migrated | "Stored for your application" | There is no scheduler, so nothing rotates on this period. |
| Network Restrictions card (only when set): IPs, origins, methods, paths | Migrated | "Stored for your application" | Always shown. |
| Allowed Scopes card (only when set), with count badge | Migrated | "Enforced by Keysmith" | An empty list reads "Any scope". |
| Keys Using This Policy, with count badge | Migrated | `keys.list` `policyId` | Paged, with a line counting the revoked ones. |
| Keys Using This Policy columns: Name, Hint, Environment, State | Migrated | | Hint and Environment are one Key column. |
| Empty state "No keys are using this policy." | Migrated | | "No keys use this policy." |

### Scopes (`/scopes`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Count badge | Migrated | `pages/scopes.tsx`, `scopes.list` | Table caption. |
| "Hierarchical permission scopes" description | Replaced | `pages/scopes.tsx` | It now says parents are stored for your application and are not used when matching, since a tree of `read` and `read:users` suggests `read` grants both, and it doesn't. |
| Tree indent (`\|--` before a child) | Dropped | | Same reason: the list is by name, and Parent is a column. |
| Column: Name (badge) | Migrated | `pages/scopes.tsx` | Mono text. |
| Column: Description ("No description") | Migrated | | |
| Column: Parent ("Root") | Migrated | | Mono, a dash when there is none. |
| Column: Created | Dropped | | The spec's scopes table is name, parent and description, sorted by name, so the date ordered nothing. |
| Empty state "No scopes defined" | Migrated | | "No scopes yet." with Create scope. Delete per row is new. |

### Rotations (`/rotations`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Count badge (rows on the page) | Replaced | `pages/rotations.tsx`, `rotations.list` | The caption names the rows shown and whether more follow; the store has no total. |
| Filter: reason (All, Scheduled, Manual, Compromise, Policy) | Migrated | `rotations.list` `reason` | |
| Column: Key (name and `****hint`, or the truncated ID) | Migrated | `components/rotation-cells.tsx` `KeyCell` | Name links to the key, with the key it became beneath. A deleted key shows its ID and "Key no longer exists". |
| Column: Reason (badge) | Migrated | `RotationReasonBadge` | Badge weights changed; see Shared components. |
| Column: Grace Period | Migrated | Grace | "None" for a zero-grace rotation. |
| Column: Grace Ends | Replaced | Window | "Window ends <time>", or "Closed", using the same rule as the key page. |
| Column: Rotated At | Migrated | When | Who rotated is under it. |
| Empty state "No rotations recorded" | Migrated | | "No rotations yet." or "No rotations match this reason." |

### Usage (`/usage`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Stat: Total Requests (all time) | Replaced | `pages/usage.tsx`, `usage.records` | The records table's caption counts the requests in the chosen range, up to 12 months. |
| Stat: Error Rate | Replaced | `usage.series` | The chart and its table split each bucket into succeeded, 4xx and 5xx. No single rate is computed for the range. |
| Stat: Avg Latency | Replaced | `usage.series` | Average latency per bucket in the table view. No single figure for the range. |
| Usage Aggregations table: Period, Requests, Errors, Avg Latency, 5xx Errors | Migrated | Table view, `usage.series` | Errors are split into 4xx and 5xx. Every bucket in the range is there, empty ones included, and buckets are UTC. |
| Errors as a red badge when above zero | Replaced | | Plain numbers. The chart carries the colour. |
| Ranges | Replaced | Range filter | Templ showed every daily bucket for every tenant. React offers 24h hourly, 7d daily, 30d daily and 12 months monthly, and a Key filter. |
| Recent Requests columns: Endpoint, Method (badge), Status (badge), Latency, IP Address, Time | Migrated | `usage.records` | Time is UTC so a row lines up with its bucket. Key is a new column. Method and Status are plain text. |
| Empty state "No usage recorded" | Migrated | | "No usage recorded yet. Usage appears once your application calls RecordUsage." |

### Usage detail (`/usage/detail?key_id=`)

The whole page is replaced by the Key filter on Usage. The filter lives in component state, not the address, so no key ID goes into the URL or its history.

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Back to Usage | Replaced | | "All keys" in the Key filter. |
| Header: "Usage: name", `prefix_****hint` | Replaced | Key filter | The filter names the key. |
| Stats (shown only with aggregates): Total Requests, Error Rate, Avg Latency, 5xx Errors | Replaced | `usage.series`, `usage.records` | As on Usage: per bucket, and the request total for the range. |
| Aggregated Usage table | Migrated | Table view | |
| Request Log (100 rows) with count badge | Migrated | Requests table, `usage.records` `keyId` | Paged, with the total in the caption. |
| Empty state "No requests have been made with this key yet." | Migrated | | "No requests recorded in this range." |

### Settings (`/settings`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| API Base Path | Dropped | | The contributor never passed it, so it always read "Default". |
| Store Driver ("connected" or "unknown") | Migrated | `pages/settings.tsx`, `settings` | Healthy or Not answering, with the store's message. |
| Routes Enabled | Dropped | | Never passed either, so it always read Enabled. |
| Auto-Migration | Dropped | | Same. |
| Registered Plugins list | Migrated | Plugins | |
| Empty state "No plugins registered." | Migrated | | "No hook plugins registered." Rate limiter, tenant, default grace and the policy enforcement table are new. |
| Plugin settings panels slot (`Plugin.DashboardSettingsPanel`) | Dropped | | The plugin interfaces are gone. |
| Host settings panel `keysmith-config` (`RenderSettings`) | Replaced | `pages/settings.tsx` | It rendered this same page. |

### Manifest and contributor

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Name, display name, icon, version | Replaced | `index.tsx` `definePlugin` | `extension: "keysmith"`, label "Keysmith". |
| Nav: Overview; Key Management (API Keys, Policies, Scopes); Analytics (Usage, Rotations); Configuration (Settings) | Replaced | `index.tsx` | One "API keys" group: Overview, Keys, Policies, Scopes, Rotations, Usage, Settings. |
| Layout `extension`, sidebar shown | Replaced | | The shell's layout. |
| Top bar: title, logo, accent `#f59e0b`, search toggle | Dropped | | The shell owns the top bar, and a plugin has no top-bar settings. |
| Top bar action and sidebar footer link "API Docs" (`/docs`) | Dropped | | The shell gives plugins neither slot. The REST API's OpenAPI docs are the host's. |
| Capability `searchable` | Dropped | | The contributor never implemented `Search`, so the flag answered no searches. |
| Widget `keysmith-stats` (Total Keys, Active) | Replaced | Overview stats | |
| Widget `keysmith-recent-keys` (5 newest, name and hint, "No keys yet.") | Replaced | Overview Recent keys | |
| Widget `keysmith-usage-summary` (Total Requests, all time, every tenant) | Replaced | Overview "Requests in the last 24h" | Reads "Not recorded" until your application records usage, so silence isn't mistaken for a quiet day. |
| `widgets/stats.templ` `StatsWidget`, `widgets/recent_keys.templ` `RecentKeysWidget` | Dropped | | Never rendered: `RenderWidget` wrote its own HTML. |
| `dashboard.Plugin` (widgets, settings panel, pages), `PluginWidget`, `PluginPage` | Dropped | | Let hook plugins render templ into the old dashboard. Nothing in forgery implements them (spec). If you need an extension point again, the React shell's sub-plugin slots are where it would go. |
| `KeyDetailContributor` | Dropped | | Same. |
| `PageContributor` | Dropped | | Same. |

### Shared components

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| `StatCard` and its icon map | Replaced | kit `StatGrid` | |
| `EmptyState` | Replaced | kit `EmptyState`, `ResourceTable` empty messages | |
| `KeyTable` | Replaced | `pages/keys.tsx` columns on kit `ResourceTable` | |
| `filterButton`, `rotationFilterButton` | Replaced | kit `FilterBar` | |
| `fieldRow` ("Not set") | Replaced | kit `DescriptionList` and `NoneCell` | |
| State badge: Active default, Rotated and Suspended secondary, Expired outline, Revoked destructive | Replaced | `badges.tsx` `KeyStateBadge` | By proportion, as the spec rules: Active outline, Suspended default, Expired and Revoked secondary, and "Expires soon" destructive for an active key that expires within 7 days. Rotated has no badge of its own because the engine never assigns it; an unknown state shows as written. |
| Environment badge: Live default, Test secondary, Staging outline | Replaced | | Text. |
| Rotation reason badge: Scheduled secondary, Manual default, Compromise destructive, Policy outline | Replaced | `badges.tsx` `RotationReasonBadge` | Manual and Scheduled outline, Policy secondary, Compromise destructive. |
| HTTP status badge (2xx default, 4xx secondary, 5xx destructive) | Replaced | | Plain numbers in the records table. |
| `formatDuration`, `formatTime`, `truncateID`, `keyAge` and the other format helpers | Replaced | `format.ts`, kit `Timestamp` | |
| `FooterAPIDocsLink` | Dropped | | See Manifest. |
| `PluginSections` | Dropped | | See Manifest. |

## Not surfaced

The engine can do these and the dashboard doesn't offer them:

- `usage.Store.Purge`
- `Engine.CleanupExpiredKeys`
- `POST /keys/validate`, on purpose: it means pasting a live key into a browser.
- Editing a key's name, description, metadata or expiry after create. The engine has no `UpdateKey`.
- `key.Store.GetByPrefix` and `key.Store.DeleteByTenant`
- `usage.Store.RecordBatch`, `DailyCount` and `MonthlyCount`
- A usage-recording middleware. Usage only exists when your application calls `RecordUsage`.
- Filtering Rotations and Usage by key from the key page. Its "View all" and "Open usage" links land unfiltered, and you pick the key again.
- The Usage page's Key filter lists the first 100 keys. A key past that still has its 7-day chart on its own page, but you can't filter the request rows down to it.
- A policy filter on the Keys list. `keys.list` takes `policyId` and the policy page uses it, but the Keys page's filter bar has environment and state only.

## Breaking changes for anyone upgrading keysmith

These landed on keysmith main between 55b057f and 3928145. If you embed keysmith, read them before you bump.

### Stores and migrations

- sqlite stores every time in UTC now (0af1673). Before that, modernc wrote local times with a monotonic reading, which don't scan back. Rows written before 0af1673 still won't scan (a key can't be read, so it never validates), and old usage rows carry local times, so they bucket in local time. You'd have to rewrite those times by hand. The store also imports `sqlitemigrate` itself now, so `New` plus `Migrate` just works.
- `keysmith_usage_agg` is dropped. Nothing ever wrote to it. Migration `20260930000002` (`drop_usage_agg` on postgres and sqlite, `drop_keysmith_usage_agg` on mongo) removes it (44850c1). `Store.Migrate` runs it too on postgres and sqlite. On mongo, `Store.Migrate` only builds indexes, so the drop runs only through the grove orchestrator (`mongostore.Migrations`). An old, empty collection is harmless if it stays.
- Other new migrations: `20260930000001` adds `old_hint` and `new_hint` to rotations and indexes `old_key_hash` (f0d6886, and df5f47d for mongo's index); `20260930000003` (`close_legacy_grace_windows`) closes the grace windows of rotations recorded before the grace fix (d361279); and on mongo, `20261005000001` (`add_keysmith_stable_sort_indexes`) adds `{tenant_id, created_at, _id}` and `{key_id, created_at, _id}` to usage and rotations (f9e328b).
- On mongo, a policy's rate-limit window, max key lifetime, rotation period and grace period read back as zero before 0edcdb7, so they were ignored. They read the stored values now. No data migration is needed, but policies you have had for a while start doing what they say.
- Mongo usage aggregation uses `$dateTrunc`, so you need MongoDB 5.0 or later (44850c1).
- Stores return not-found errors that `errors.Is` matches against `keysmith.ErrKeyNotFound` and friends (6106369). Code that string-matched the old private errors will stop matching.
- Lists page in a stable order on every backend: keys, policies, rotations and usage by `created_at DESC, id DESC`, scopes by name and then id (8b62464, 226f56a, 822e68c, a8799a1). The memory store's usage query is newest first now. It used to be oldest first.
- On memory and mongo, deleting a scope takes it off every key in its tenant (1c84546). Postgres and sqlite already cascaded. Before this, re-creating a deleted scope's name on memory handed it back to every key that once held it.

### Engine behaviour

- Rotation keeps the previous key valid for a grace window (7300130). It used to die at once, whatever `grace_ends` said. The window comes from `WithGrace`, then the policy's `GracePeriod`, then 24 hours. If you relied on the old key stopping immediately, as you would after a compromise, pass `WithGrace(0)`. Rotation records written before the fix never open a window (d361279).
- `RotateKey` refuses a revoked or expired key with `ErrInvalidStateTransition`, and `RevokeKey` ends every open window on the key (7300130).
- Revocation is terminal (74becf8). `SuspendKey` takes only an active key, and `RevokeKey` refuses a key that is already revoked, so the revoked hook fires once. `RevokedAt` wins over `State` everywhere the engine can see it (09eec0a).
- `CreateKey` and `AssignScopes` refuse a scope name that doesn't exist in the key's tenant (`ErrScopeNotFound`) or that the key's policy doesn't allow (`ErrScopeNotAllowed`), before anything is written (5c293d9). This breaks authsome's `bridge/keysmithadapter` on its next keysmith bump: it passes scope names it never creates.
- `CreateKey` refuses an explicit expiry past the policy's `MaxKeyLifetime` with `ErrKeyLifetimeExceeded` (6e2eebe).
- A policy store that can't be read fails `ValidateKey` and `RotateKey` for keys that have a policy, where it used to carry on with no policy (93bcaab). A policy that is simply gone still validates the key without one.
- Policy and scope names are unique per tenant on every backend: `ErrPolicyNameTaken` and `ErrScopeNameTaken` (e4e4a67). Memory used to take duplicates, and the SQL and mongo stores answered a raw driver error.
- `DeletePolicy` ignores revoked keys, so a policy that only revoked keys use can go (bf151c9, 252b2d7). `ErrPolicyInUse` keeps its identity, but its text changed from "policy is assigned to active keys" to "policy is used by keys that are not revoked". Match it with `errors.Is`, not the string.
- `DeleteScope` refuses while another scope in the tenant names it as parent (`ErrScopeHasChildren`, 252b2d7) or while a policy in the tenant lists it in `allowedScopes` (`ErrScopeAllowedByPolicy`, 8abe1da).
- `usage.Store.Aggregate` groups the raw `keysmith_usage` rows on every call (44850c1). The period must be `hourly`, `daily` or `monthly`, or you get `usage.ErrInvalidPeriod`. Buckets are UTC. `After` is inclusive and `Before` exclusive on every backend (memory's `Before` used to be inclusive, which also changes its `Query` and `Count`). Only buckets with rows come back. Leave out the key and you get sums across keys with a zero key ID. `Aggregate` ignores `Limit` and `Offset`, so bound the range with `After` and `Before` (93bcaab).

### Types and interfaces

- `rotation.Store` gained `GetInGraceByOldHash` and `EndGrace` (f0d6886). A custom store won't compile until it implements both.
- `usage.Aggregation.P50Latency` and `P99Latency` are `*int64` now, are never set, and drop out of the JSON (44850c1). The four backends can't compute the same percentile. `ServerErrorCount` is new.
- `Engine.CleanupGraceExpired` is removed (7300130). It never acted, and with its filter corrected it would have revoked live keys.
- The `dashboard` package is deleted, and the extension no longer has `DashboardContributor()` (c08cf0f).
- Additions that compile unchanged: `RotateKey(ctx, id, reason, opts ...RotateOption)` with `WithGrace` and `WithRotatedBy`; `Engine.EndGrace`; `Engine.RateLimiterConfigured`; `ValidationResult.ViaPreviousKey` and `GraceEnds`; `rotation.Record.OldHint` and `NewHint`. `key.StateRotated` stays defined, and the engine never assigns it.

### REST API

- Missing keys, policies, scopes and rotations answer 404 where they answered 500 (6106369).
- 409 for revoking a key twice, including a repeated `DELETE /keys/:id` that used to succeed (74becf8), and for a duplicate policy or scope name and a refused scope delete (af19505).
- 400 for `ErrKeyLifetimeExceeded` and for an unknown usage period.
- The `/usage` endpoints return data now. They read the table nothing wrote before 44850c1.

## Open findings for the maintainer

Reported during the migration and not fixed. One line each, with where it lives.

- The forge dashboard's idempotency cache keeps every `keys.create` and `keys.rotate` response, raw key included, in server memory for 24 hours, replayable with the same idempotency key (forge: `extensions/dashboard/extension.go` wires `idempotency.NewInMemoryStore`, and the dispatcher hardcodes 24h). Keysmith can't opt out.
- `CreateKey` isn't atomic: if `AssignToKey` fails after `Keys().Create`, a live key with no scopes stays behind (`engine.go`, `CreateKey`).
- There is no compare-and-swap on key `Update`, so a rotate racing a revoke can write a stale row. 09eec0a only guards on `RevokedAt` (`engine.go` `RotateKey`, every key store's `Update`).
- A compromise rotation (`WithGrace(0)`) leaves the key's earlier grace windows open, so you have to call `EndGrace` too (`engine.go`, `RotateKey`).
- If the key update fails after its rotation record is written, the orphan record can shadow a later zero-grace rotation of the same old hash. The latest-created record for an old hash should decide (`engine.go` `RotateKey`, `GetInGraceByOldHash` in each rotation store).
- `ValidateKey` treats a `GetByHash` store outage as a miss and answers `ErrInvalidKey` (`engine.go`, `ValidateKey`).
- `FireKeyValidationFailed` hands the presented raw key to every plugin, including for a key that was found but is inactive (`engine.go` `ValidateKey`, `plugin/manager.go`).
- On sqlite, the background `UpdateLastUsed` that `ValidateKey` starts can hit `SQLITE_BUSY` against the next write, and a test flakes about 3 runs in 40 (`engine.go` `ValidateKey`, `store/sqlite`).
- On sqlite, `PRAGMA foreign_keys=ON` runs on one pooled connection only, so `ON DELETE CASCADE` is skipped at random and orphan `keysmith_key_scopes` rows stay (grove's sqlite driver sets it at open, and `store/sqlite` never caps the pool).
- The memory store's `ListByKey` matches scope names across tenants, and its `AssignToKey` checks neither existence nor tenant (`store/memory`, scope store).
- `DeletePolicy` and `DeleteScope` check and then delete without a transaction, so a key or child scope created in between dangles (`engine.go`).
- Two writes that race past the name check hit the unique index and surface as `INTERNAL` on the contract and 500 on REST (`engine.go` `checkPolicyName` and `CreateScope`, the stores' `(tenant_id, name)` indexes).
- Deleting a key on postgres or sqlite cascades its rotation history (`store/postgres` and `store/sqlite` migrations, `keysmith_rotations.key_id ... ON DELETE CASCADE`).
- `keysmith_rotations` has no `tenant_id` index on postgres or sqlite, so the Rotations page scans and sorts (`store/postgres`, `store/sqlite` migrations).
- REST `createKey` and `rotateKey` skip `mapStoreError`, so their refusals answer 500, and `listKeys` and the `/usage` routes set no tenant filter, so they answer every tenant's rows (`api/key_handler.go`, `api/usage_handler.go`).
- The key middleware writes the engine's error text into its response, which can carry a store error (`middleware/middleware.go`).
- forge's binder treats every REST create field as required (no scope without a parent, no policy without every duration), and the create handlers call `ctx.JSON` and also return the response, so the body is written twice (`api/*_handler.go`).
- authsome has its own API keys (`plugins/apikey`), and its `WithKeysmith` adapter builds a keysmith `KeyManager` nothing calls. Keys minted through it have no tenant, so the dashboard never shows them (authsome).
- `warden_hook` needs a `WardenBridge` that nothing in forgery implements, and reacts to create and revoke only, not to scope changes, reactivation or rotation (`warden_hook/extension.go`).
- Open question: should `tenantFrom` prefer the forge Scope's org over `dashboard.tenant_id`? Under authsome the dashboard shows the configured tenant, not the session's org (`extension/contract/tenant.go`).
- Hooks get the request context, so a hook reading `forge.ScopeFrom` sees the session's org, not the contract's tenant. Only `CreateKey`, `CreatePolicy` and `CreateScope` run under `engineCtx` (`extension/contract`).
- Waiting on confirmation: `scopes.delete` also refusing while a policy allows the scope was a controller ruling, made to match the parent rule (8abe1da).
