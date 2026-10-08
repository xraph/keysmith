# Migrating to the React dashboard

## What changed

Keysmith's dashboard is now the React package `@forge-go/dashboard-plugin-keysmith`, which lives in the forge-dashboard repository (`packages/plugin-keysmith`) and talks to keysmith through the contract contributor the extension registers in `Extension.RegisterContractContributor`. The Go-rendered templ dashboard under `dashboard/` is gone, and so is `Extension.DashboardContributor()`, which c08cf0f removed once forge main dropped the contributor package. To turn the new one on you run forge's dashboard extension with the React shell. Keysmith picks the tenant for each request in this order (a87f984): the principal's `tenant_id` claim (nothing sets one today), then the org of the forge Scope on the request, which an auth extension such as authsome sets from the session, then the tenant you configure, then a refusal. So under authsome every session sees its own org, and the configured tenant only serves a session with no org (an app-only session, or no auth extension at all). To configure it, set `extensions.keysmith.dashboard.tenant_id` (and, if you want new rows labelled, `app_id`) in YAML, or pass `WithDashboardTenant(tenantID, appID)` when you build the extension, and YAML wins when both are set. A request with neither an org nor a configured tenant answers `PERMISSION_DENIED` with a message naming that config key, because keysmith won't guess. Settings reports where the tenant came from, in `tenantSource`: `claim`, `scope` or `config`. Every page shows one tenant. The templ pages set no tenant at all, so they showed you every tenant's rows, and rows written with an empty tenant (standalone use), or under another one (authsome's `apikey.KeysmithStore` stamps its app ID, see Open findings), can't be reached from the new dashboard.

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
- Templ lists read one fixed page (50 keys, 100 policies, 200 scopes, 50 rotations, 50 usage records) with no paging. In React, keys, rotations and usage records page. Policies and scopes read the first 200 and say when there are more.

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
| Search box "Search keys by name" | Dropped | | It sent `name`, and nothing read it: `renderKeys` ignored the parameter, so the box never filtered anything. `keys.list` has no name filter. You narrow the list by environment, state or policy instead (`keys.list` `policyId`). |
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
| Stat: Usage (all-time request count) | Dropped | | No page keeps an all-time count now. The Usage section shows the key's last 7 UTC days with a compact chart, and Open usage covers any range up to 12 months. |
| Stat: Age | Replaced | Details "Created" | |
| Stat: Rotations (count) | Replaced | Rotation history section | Lists the 10 newest and says when there are more. |
| Stat: Last Used | Migrated | Details "Last used" | |
| Policy card: name, description | Migrated | Policy section, `keys.detail` | The name links to the policy page, where the description is. |
| Policy card: max lifetime, grace period | Migrated | Policy section | |
| Policy card: rate limit, burst, daily and monthly quota, rotation period | Replaced | `pages/policy-detail.tsx` | The key page names what Keysmith enforces. The rest sits on the policy page under the group that says who enforces it. |
| Assigned Scopes card, with count badge | Replaced | `components/scopes-editor.tsx`, `keys.scopes.assign`, `keys.scopes.remove` | You can add and remove scopes here now. |
| Empty state "No scopes assigned." | Replaced | Scopes section | A dash (`NoneCell`, read as "no scopes"). |
| Recent Usage table (20 rows: endpoint, method, status, latency, time) | Replaced | `pages/usage.tsx`, `usage.records` | The key page shows the chart. Open usage shows the rows on Usage with this key already chosen. |
| Recent Usage: View All (to `/usage/detail?key_id=`) | Migrated | "Open usage", `/@keysmith/usage?keyId=` | Opens Usage with the key chosen in its Key filter. |
| Empty state "No usage recorded yet." | Migrated | Usage section | "Usage appears once your application calls RecordUsage." |
| Rotation History table: Reason, Grace Period, Grace Ends, Rotated At | Migrated | Rotation history section, `rotations.list` `keyId` | Each row shows the old and new key and whether its window is open. Grace Period is the Grace column on Rotations. View all opens Rotations narrowed to this key (`?keyId=`). |
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
| Stat: Total Requests (all time) | Dropped | | Nothing counts all time. The range summary above the chart counts the requests in the chosen range, up to 12 months. |
| Stat: Error Rate | Replaced | `pages/usage.tsx` range summary, `usage.series` | 4xx plus 5xx over requests for the chosen range, next to the 5xx count. The chart and its table split each bucket the same way. |
| Stat: Avg Latency | Replaced | `pages/usage.tsx` range summary, `usage.series` | Average latency over the range, weighted by requests. The table view has it per bucket. |
| Usage Aggregations table: Period, Requests, Errors, Avg Latency, 5xx Errors | Migrated | Table view, `usage.series` | Errors are split into 4xx and 5xx. Every bucket in the range is there, empty ones included, and buckets are UTC. |
| Errors as a red badge when above zero | Replaced | | Plain numbers. The chart carries the colour. |
| Ranges | Replaced | Range filter | Templ showed every daily bucket for every tenant. React offers 24h hourly, 7d daily, 30d daily and 12 months monthly, and a Key filter. |
| Recent Requests columns: Endpoint, Method (badge), Status (badge), Latency, IP Address, Time | Migrated | `usage.records` | Time is UTC so a row lines up with its bucket. Key is a new column. Method and Status are plain text. |
| Empty state "No usage recorded" | Migrated | | "No usage recorded yet. Usage appears once your application calls RecordUsage." |

### Usage detail (`/usage/detail?key_id=`)

The whole page is replaced by the Key filter on Usage, which takes its key from the address: `/@keysmith/usage?keyId=`. The key page's Open usage link lands there. Only the key goes in the URL. Picking another key is a navigation that replaces the current history entry (forge-dashboard 2037b27 gave plugins `useNavigateTo(path, { replace })`, and cb91eb1 uses it), so Back leaves the page instead of stepping through the keys you tried. The key page's Open usage and View all links are ordinary links and still push. A key past the first 100, which the select doesn't list, still shows as the chosen key.

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| Back to Usage | Replaced | | "All keys" in the Key filter. |
| Header: "Usage: name", `prefix_****hint` | Replaced | Key filter | The filter names the key. |
| Stats (shown only with aggregates): Error Rate, Avg Latency, 5xx Errors | Replaced | Range summary, `usage.series` | As on Usage, for the key and the chosen range. |
| Stats: Total Requests (all time) | Dropped | | As on Usage: nothing counts all time, and the range summary counts up to 12 months. |
| Aggregated Usage table | Migrated | Table view | |
| Request Log (100 rows) with count badge | Migrated | Requests table, `usage.records` `keyId` | Paged, with the total in the caption. Works for any key, including one past the first 100. |
| Empty state "No requests have been made with this key yet." | Migrated | | "No requests recorded in this range." |

### Settings (`/settings`)

| Templ element | Status | Where it lives now | Note |
|---|---|---|---|
| API Base Path | Dropped | | The contributor never passed it, so it always read "Default". |
| Store Driver ("connected" or "unknown") | Migrated | `pages/settings.tsx`, `settings` | Healthy or Not answering, with a fixed line. The driver's error stays in the server log. |
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
| Widget `keysmith-usage-summary` (Total Requests, all time, every tenant) | Replaced | Overview "Requests in the last 24h" | Reads "Not recorded" until your application records usage, so silence isn't mistaken for a quiet day. The all-time count is gone, as on Usage. The templ widgets refreshed themselves every 15 to 60 seconds. The overview has no auto-refresh and reads once per visit. |
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
- Request totals over all time. Every count is for a range: the last 24h on the overview, 7 days on a key's page, and up to 12 months on Usage.

## Breaking changes for anyone upgrading keysmith

These landed on keysmith main after 55b057f, up to and including 05ef9ca, which deleted the templ dashboard. The sqlite DSN entry came later, in afcf71c, and the tenant order, key version and conflict entries came after that, from a87f984 to e509c1b. The forge bumps came last, in f5a154a (v1.12.1) and 2e6a21f (v1.12.2). If you embed keysmith, read them before you bump.

### Forge version

- keysmith needs forge v1.12.2 now. It was on v1.10.0, went to v1.12.1 in f5a154a and to v1.12.2 in 2e6a21f. `extension/contract` registers `keys.create` and `keys.rotate` with `dispatcher.SecretResponse()`, which forge added in v1.12.1 (3aff4b48 on the v1.12.1 tag, c6461e5f on forge main), so it won't compile against an older forge. When you bump keysmith, Go lifts your forge to v1.12.2 too, along with go-utils v1.3.0 and confy v1.0.3. The step from v1.12.1 to v1.12.2 moved nothing else in keysmith's module graph. Forge v1.12 removed the templ dashboard: `extensions/dashboard/contributor`, `pages`, `layouts`, `settings`, `search`, `sse`, `proxy`, `recovery`, `assets`, `contract/components` and `contract/shell` are gone, and keysmith no longer pulls in templ or forgeui. So every other extension in the same app has to be on the contract dashboard (forge v1.12 or later) before you bump keysmith. One that still uses the templ dashboard API stops compiling the moment keysmith lifts forge.
- A dashboard client that sends `keys.create` or `keys.rotate` again with the same idempotency key gets `CONFLICT` now, with forge's message: "command already ran and its response held a secret that is not kept; send a new idempotency key to run it again". It used to get the first response back, raw key and all, from a cache that held it for 24 hours. The cache keeps a tombstone (status 409, no body) in its place, and the command doesn't run a second time. Every other command still replays its first answer.
- Two overlapping dispatches with one idempotency key and user run the command once now. Since v1.12.2 (840b8a4a and 4b7f6e8c on forge main) the dispatcher claims the key before the handler runs and holds it until the entry is stored. A second `keys.create` or `keys.rotate` that arrives in the meantime waits for the first, 10 seconds by default, and then answers `CONFLICT`: the message above once the first has finished, or a retryable "the same command is still running under this idempotency key; retry once it finishes" if it hasn't. On v1.12.1 both ran and minted two keys.

### Stores and migrations

- sqlite stores every time in UTC now (0af1673). Before that, modernc wrote local times with a monotonic reading, which don't scan back. Rows written before 0af1673 still won't scan (a key can't be read, so it never validates), and old usage rows carry local times, so they bucket in local time. You'd have to rewrite those times by hand. The store also imports `sqlitemigrate` itself now, so `New` plus `Migrate` just works.
- `keysmith_usage_agg` is dropped. Nothing ever wrote to it. Migration `20260930000002` (`drop_usage_agg` on postgres and sqlite, `drop_keysmith_usage_agg` on mongo) removes it (44850c1). `Store.Migrate` runs it too on postgres and sqlite. On mongo, `Store.Migrate` only builds indexes, so the drop runs only through the grove orchestrator (`mongostore.Migrations`). An old, empty collection is harmless if it stays.
- Other new migrations: `20260930000001` adds `old_hint` and `new_hint` to rotations and indexes `old_key_hash` (f0d6886, and df5f47d for mongo's index); `20260930000003` (`close_legacy_grace_windows`) closes the grace windows of rotations recorded before the grace fix (d361279); and on mongo, `20261005000001` (`add_keysmith_stable_sort_indexes`) adds `{tenant_id, created_at, _id}` and `{key_id, created_at, _id}` to usage and rotations (f9e328b).
- Keys have a `version` column now (d207450). Migration `20261007000001` adds it: `key_version` on postgres and sqlite (`ALTER TABLE keysmith_keys ADD COLUMN version BIGINT NOT NULL DEFAULT 0`, which `Store.Migrate` runs too), and `add_keysmith_keys_version` on mongo, which sets `version` to 0 where it is missing. On mongo, `Store.Migrate` only builds indexes, so the migration runs only through the grove orchestrator; a database that skips it still works, because a missing field reads as 0. A custom store has to persist the column.
- On mongo, a policy's rate-limit window, max key lifetime, rotation period and grace period read back as zero before 0edcdb7, so they were ignored. They read the stored values now. No data migration is needed, but policies you have had for a while start doing what they say.
- Open a sqlite database with `sqlite.DSN(path)` (afcf71c). It gives every pooled connection a 5 second busy timeout, foreign keys on and `BEGIN IMMEDIATE` transactions. Before, grove's driver turned foreign keys on for one connection out of ten and set no busy timeout, so `ON DELETE CASCADE` ran at random and the write after a `ValidateKey` could fail with `SQLITE_BUSY`. The store takes a `*grove.DB` it didn't open, so it can't do this for you. If a grove extension's config opens the database, add `_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate` to that DSN yourself.
- Mongo usage aggregation uses `$dateTrunc`, so you need MongoDB 5.0 or later (44850c1).
- Stores return not-found errors that `errors.Is` matches against `keysmith.ErrKeyNotFound` and friends (6106369). Code that string-matched the old private errors will stop matching.
- Lists page in a stable order on every backend: keys, policies, rotations and usage by `created_at DESC, id DESC`, scopes by name and then id (8b62464, 226f56a, 822e68c, a8799a1). The memory store's usage query is newest first now. It used to be oldest first.
- On memory and mongo, deleting a scope takes it off every key in its tenant (1c84546). Postgres already cascaded, and so does sqlite on a connection with foreign keys on, which `sqlite.DSN` gives every connection (afcf71c). Before this, re-creating a deleted scope's name on memory handed it back to every key that once held it.

### Engine behaviour

- Rotation keeps the previous key valid for a grace window (7300130). It used to die at once, whatever `grace_ends` said. The window comes from `WithGrace`, then the policy's `GracePeriod`, then 24 hours. If you relied on the old key stopping immediately, as you would after a compromise, pass `WithGrace(0)`. Rotation records written before the fix never open a window (d361279).
- `RotateKey` writes the rotation record before it replaces the key's hash (e2bb734). If the record write fails, the key is untouched and you get an error. Before, the old key could already be dead with no new key returned. The rotated hook fires only after both writes succeed.
- `RotateKey` refuses a revoked or expired key with `ErrInvalidStateTransition`, and `RevokeKey` ends every open window on the key (7300130).
- Revocation is terminal (74becf8). `SuspendKey` takes only an active key, and `RevokeKey` refuses a key that is already revoked, so the revoked hook fires once. `RevokedAt` wins over `State` everywhere the engine can see it (09eec0a).
- `CreateKey` and `AssignScopes` refuse a scope name that doesn't exist in the key's tenant (`ErrScopeNotFound`) or that the key's policy doesn't allow (`ErrScopeNotAllowed`), before anything is written (5c293d9). authsome's `bridge/keysmithadapter` passes scope names it never creates, so it would fail on these checks if anything called it. Nothing does today (see Open findings).
- The dashboard's tenant comes from the session's org now (a87f984). Keysmith reads, in order, the principal's `tenant_id` claim, the org of the forge Scope on the request (set by an auth extension such as authsome), the configured `dashboard.tenant_id`, and then refuses. A deployment with orgs used to show every session the configured tenant; each session now sees its own org's rows, and the configured tenant's rows go to sessions with no org. An app-only session (a Scope with an empty org) still gets the configured tenant. Settings reports the source in `tenantSource`: `claim`, `scope` or `config`.
- `CreateKey` deletes the new key when its scopes fail to attach (db76dee), so a failed create no longer leaves a live key with no scopes. The error you get is still the attach error.
- `RotateKey`, `RevokeKey`, `SuspendKey` and `ReactivateKey` write through `key.Store.UpdateIfVersion` and answer `ErrKeyConflict` when the key changed since they read it (d207450). A rotate that races a revoke can't write its stale row any more. A rotation that loses the race ends the window on its own record, by id, so that record can't hold the old key's window open past what the winner chose (e509c1b). `SuspendKey` and `ReactivateKey` write the whole row now, where they set only the state before, and the reactivated hook receives the key with `State` active. Retry a conflict from a fresh read.
- `CreateKey` refuses an explicit expiry past the policy's `MaxKeyLifetime` with `ErrKeyLifetimeExceeded` (6e2eebe).
- A policy store that can't be read fails `ValidateKey` and `RotateKey` for keys that have a policy, where it used to carry on with no policy (93bcaab). A policy that is simply gone still validates the key without one.
- Policy and scope names are unique per tenant on every backend: `ErrPolicyNameTaken` and `ErrScopeNameTaken` (e4e4a67). Memory used to take duplicates, and the SQL and mongo stores answered a raw driver error.
- `DeletePolicy` ignores revoked keys, so a policy that only revoked keys use can go (bf151c9, 252b2d7). `ErrPolicyInUse` keeps its identity, but its text changed from "policy is assigned to active keys" to "policy is used by keys that are not revoked". Match it with `errors.Is`, not the string.
- `DeleteScope` refuses while another scope in the tenant names it as parent (`ErrScopeHasChildren`, 252b2d7) or while a policy in the tenant lists it in `allowedScopes` (`ErrScopeAllowedByPolicy`, 8abe1da). The maintainer confirmed the policy rule, so it is decided and not a finding.
- `usage.Store.Aggregate` groups the raw `keysmith_usage` rows on every call (44850c1). The period must be `hourly`, `daily` or `monthly`, or you get `usage.ErrInvalidPeriod`. Buckets are UTC. `After` is inclusive and `Before` exclusive on every backend (memory's `Before` used to be inclusive, which also changes its `Query` and `Count`). Only buckets with rows come back. Leave out the key and you get sums across keys with a zero key ID. `Aggregate` ignores `Limit` and `Offset`, so bound the range with `After` and `Before` (documented in 93bcaab).

### Types and interfaces

- `rotation.Store` gained `GetInGraceByOldHash` and `EndGrace` (f0d6886), and `EndGraceByID` (e509c1b). A custom store won't compile until it implements all three.
- `key.Key` has a `Version` field, and `key.Store` gained `UpdateIfVersion(ctx, key, version)` (d207450). A custom key store won't compile until it implements it, and has to keep the counter: `Update` and `UpdateState` add one, `UpdateLastUsed` leaves it alone, and plain `Update` stays unconditional so a caller that writes keys directly, such as authsome's `apikey.KeysmithStore`, keeps working. `ErrKeyConflict` (also `store.ErrKeyConflict`) is the new error.
- `usage.Aggregation.P50Latency` and `P99Latency` are `*int64` now, are never set, and drop out of the JSON (44850c1). The four backends can't compute the same percentile. `ServerErrorCount` is new.
- `Engine.CleanupGraceExpired` is removed (7300130). It never acted, and with its filter corrected it would have revoked live keys.
- The `dashboard` package is gone, and the extension no longer has `DashboardContributor()`. c08cf0f stopped wiring the package and removed that method, and 05ef9ca deleted the package itself.
- Additions that compile unchanged at call sites: `RotateKey(ctx, id, reason, opts ...RotateOption)` with `WithGrace` and `WithRotatedBy` (an interface of your own that names the old three-argument signature needs the variadic); `Engine.EndGrace`; `Engine.RateLimiterConfigured`; `ValidationResult.ViaPreviousKey` and `GraceEnds`; `rotation.Record.OldHint` and `NewHint`. `key.StateRotated` stays defined, and the engine never assigns it.

### REST API

- Missing keys, policies, scopes and rotations answer 404 where they answered 500 (6106369).
- 409 for revoking a key twice, including a repeated `DELETE /keys/:id` that used to succeed, and for suspending a key that isn't active, which used to succeed even on a revoked key (74becf8). Duplicate policy or scope names and refused scope deletes answer 409 too (af19505).
- 409 for `ErrKeyConflict`, from rotate, revoke, suspend and reactivate when the key changed under them (d207450). The contract answers `CONFLICT`.
- 400 for `ErrKeyLifetimeExceeded` and for an unknown usage period.
- The `/usage` endpoints return data now. They read the table nothing wrote before 44850c1.

### Which backends the tests cover

Plain `go test ./...` runs the store tests against memory and sqlite only. To cover postgres and mongo as well, run `make test-backends`. It needs Docker, starts throwaway `keysmith-test-pg` and `keysmith-test-mongo` containers on odd ports, runs the whole suite against all four backends and removes the containers when it's done. If you'd rather use servers you already have, set `KEYSMITH_TEST_PG_DSN` or `KEYSMITH_TEST_MONGO_URI` (or both) before `go test ./...`, and each one adds its backend.

## Open findings for the maintainer

Reported during the migration and not fixed. One line each, with where it lives.

- The idempotency fixes in forge v1.12.1 and v1.12.2 (see Forge version) leave two gaps. A claim lasts one minute and nothing renews it, so a `keys.create` or `keys.rotate` whose handler runs longer lets a duplicate with the same key run beside it and mint a second key. And the store is `idempotency.NewInMemoryStore`, one per process, so replicas behind a load balancer share neither claims nor tombstones: a retry that lands on another replica runs again (forge: `extensions/dashboard/contract/idempotency`, and `extensions/dashboard/extension.go`, which wires the store).
- Forge keys idempotency on `user:intent` with no org, so one user's requests in two orgs share a key space (forge: `extensions/dashboard/contract/dispatcher`).
- A compromise rotation (`WithGrace(0)`) leaves the key's earlier grace windows open, so you have to call `EndGrace` too (`engine.go`, `RotateKey`).
- If the key update fails after its rotation record is written, the orphan record can shadow a later zero-grace rotation of the same old hash. The latest-created record for an old hash should decide (`engine.go` `RotateKey`, `GetInGraceByOldHash` in each rotation store).
- `ValidateKey` treats a `GetByHash` store outage as a miss and answers `ErrInvalidKey` (`engine.go`, `ValidateKey`).
- `FireKeyValidationFailed` hands the presented raw key to every plugin, including for a key that was found but is inactive (`engine.go` `ValidateKey`, `plugin/manager.go`).
- The memory store's `ListByKey` matches scope names across tenants, and its `AssignToKey` checks neither existence nor tenant (`store/memory`, scope store).
- `DeletePolicy` and `DeleteScope` check and then delete without a transaction, so a key or child scope created in between dangles (`engine.go`).
- Two writes that race past the name check hit the unique index and surface as `INTERNAL` on the contract and 500 on REST (`engine.go` `checkPolicyName` and `CreateScope`, the stores' `(tenant_id, name)` indexes).
- Deleting a key on postgres or sqlite cascades its rotation history (`store/postgres` and `store/sqlite` migrations, `keysmith_rotations.key_id ... ON DELETE CASCADE`).
- `keysmith_rotations` has no `tenant_id` index on postgres or sqlite, so the Rotations page scans and sorts (`store/postgres`, `store/sqlite` migrations).
- REST `createKey` and `rotateKey` skip `mapStoreError`, so their refusals answer 500, and `listKeys` and the `/usage` routes set no tenant filter, so they answer every tenant's rows (`api/key_handler.go`, `api/usage_handler.go`).
- The key middleware writes the engine's error text into its response, which can carry a store error (`middleware/middleware.go`).
- forge's binder treats every REST create field as required (no scope without a parent, no policy without every duration), and the create handlers call `ctx.JSON` and also return the response, so the body is written twice (`api/*_handler.go`).
- authsome's `WithKeysmith` makes `apikey.KeysmithStore` write keys straight into keysmith's store. That skips the engine, so there are no policy or scope checks and no hooks, and it sets `TenantID` to the authsome app ID while the engine and the dashboard use the org. A session with an org won't see an authsome-issued key in the dashboard. Its `keysmithadapter` `KeyManager` is never called. It writes with plain `Update`, so the version check doesn't break it (authsome `apikey/keysmith_store.go` for `KeysmithStore`, `bridge/keysmithadapter` for the `KeyManager`).
- `warden_hook` has an implementer now: `github.com/xraph/warden/keysmithbridge` (warden 02116cf and 75f6566, on warden's `soc2-hardening` branch). It reads a scope action first, the way keysmith writes it, and splits at the first colon: `read:users` becomes the warden permission `users:read`, with resource `users` and action `read`. A parent scope with no colon, such as `read`, is skipped, because it covers every resource and warden could only say that with a wildcard. It refuses a scope with `*`, and never creates roles, so a tenant without the hook's default `api-key` role logs a warning for every key. The hook still ignores scope changes, reactivation and rotation (`warden_hook/extension.go`).
- The dashboard's tenant follows the session on the server. If you switch org in another tab, the keysmith pages keep showing cached rows from the old org until they refetch. The tenant check refuses any command on those rows, so the stale view is display only (forge-dashboard `packages/plugin-keysmith`, the query store in `packages/plugin/src/store.ts`).
- Hooks get the request context, so a hook reading `forge.ScopeFrom` sees the session's org, and that differs from the contract's tenant in two cases: a `tenant_id` claim names another tenant, or the session has no org (an app-only session, or no auth extension), where the contract uses the configured tenant. Only `CreateKey`, `CreatePolicy` and `CreateScope` run under `engineCtx` (`extension/contract`).
