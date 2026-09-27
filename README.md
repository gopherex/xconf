# xconf

Configuration loading and live reload against a [schemapb](https://github.com/gopherex/schemapb) schema.
This is the v1 API. The module remains `github.com/gopherex/xconf`; the previous DSL,
code generator and `pkg/structconf` API are removed. Existing applications can stay
on their pinned releases until migrated.

## Contract

Sources supply partial raw layers, ordered from lowest to highest priority.
Objects and maps merge recursively. Scalars, null and lists replace previous values.
Explicit zero, false, empty string and empty list are present values. An empty object
preserves lower keys; `Edit{Kind: Replace}` clears/replaces an object. `Delete` removes
a key from the assembled input; a schema default can subsequently restore it.
Changing a OneOf discriminator replaces the previous variant's entire object.

Only after all layers merge does schemapb run defaults, coercion, normalization,
computed fields, validation and canonicalization. Enable coercion in the schema
for textual sources (reflection already enables it). Unknown-field policy belongs
to the schema's strict setting. An absent section is created only with an explicit
Object/Ref `default={}`. Null is never treated as absence.

## Loading

```go
schema, err := sp.ReflectType[Config](sp.ID("app", "config", sp.Ver(1, 0, 0)))
if err != nil { return err }
snapshot, err := xconf.Load(ctx, schema,
    yaml.File("config.yaml"),
    xconf.Optional(yaml.File("config.local.yaml")),
    env.New(env.Prefix("APP_")),
)
if err != nil { return err }
cfg, err := xconf.Decode[Config](snapshot)
```

`LoadAs[T]` returns the decoded configuration directly. `Load` retains diagnostics,
source revisions and `Explain("db", "port")` metadata. Explanation records contain
source writes and schema operations, never values. Paths use JSON pointer encoding;
map keys containing dots or slashes remain unambiguous.

`New(schema, compileOptions...)` builds a reusable Loader with custom schemapb
formats or CEL cost limits. Its Load/Open methods reuse the compiled engine.

A runnable typed example is in `example/`: run `GOWORK=off go run . -once` there.
Run without `-once` and edit `config.json` to see reloads; `APP_SERVER_PORT` overrides
its port. The example module is tested by CI and excluded from library release tags.

## Live configuration

`Open` returns an untyped runtime; `OpenAs[T]` also checks decoding before publishing.
`TypedRuntime.Current()` returns `(T, error)` with independent maps and slices.
`Snapshot()` is immutable through its public API; Baked/Report/Validation accessors
and Decode copy owned state. Custom decode hooks should be deterministic.

```go
runtime, err := xconf.OpenAs[Config](ctx, schema,
    yaml.File("config.yaml"), env.New(env.Prefix("APP_")),
)
if err != nil { return err }
defer runtime.Close()
for event := range runtime.Subscribe(ctx) {
    if event.Err != nil { /* retain the currently applied settings */ continue }
    cfg, err := xconf.Decode[Config](event.Snapshot)
    if err != nil { return err }
    _ = cfg // application owns component reconfiguration
}
```

Watchers register before the initial read. Initial failure aborts Open. Reloads are
serialized, rebuild from source layers and retain the last valid snapshot on read,
merge, validation or decode failure. `Reload(ctx)` requests a manual rebuild.
Source removal therefore reveals lower-priority values. Changes during a rebuild
schedule another pass. Notifications are coalesced over a 20ms window.

Subscribers receive the current snapshot immediately. Delivery never blocks reload;
each subscriber retains only its latest event. Changed paths compare consecutive
published snapshots, so a subscriber skipping versions must reconcile full state.
An error event includes the last valid snapshot. Successful unchanged reads do not
increment the version; provenance/revision changes do, even if values are identical.
Recovery after a failed read emits a successful event even when the version stays unchanged.
Cancellation closes subscriptions. Close cancels reads and stops watchers; source
implementations must honor context. Atomic publication is process-local, not a
transaction across unrelated external stores.

## Sources

Each contrib is a separate Go module. Import only the integrations you use:

| Module under `contrib/sources/` | Input |
| --- | --- |
| `json` | JSON object, exact numeric tokens |
| `yaml` | One YAML mapping, aliases supported with bounded expansion |
| `toml` | TOML document |
| `env` | Process or injected environment, schema-based names |
| `dotenv` | .env file; does not modify process environment |
| `flags` | Explicit argument slice and name-to-path bindings |
| `file` | Custom document decoder and file lifecycle |
| `fs` | Any `fs.FS`, including embedded defaults |
| `http` | HTTP document with ETag / Last-Modified revalidation |
| `directory` | One string value per file, including Docker/Kubernetes secrets |
| `s3` | S3-compatible object storage, optional pinned version |
| `vault` | Vault KV v2 object, optional pinned version |
| `nats` | One document in a JetStream KV key, native watch |
| `consul` | One document or a prefix of KV fields, blocking-query watch |
| `etcd` | One document in an etcd v3 key, revision watch |
| `kubernetes` | Named ConfigMap/Secret data or document, API watch |
| `pflag` | Snapshot of explicitly changed pflag/Cobra flags |
| `reader` | Repeatable reader factory or captured stream |
| `envfile` | Explicit `*_FILE` variables pointing to secret files |

Transport-independent decoders are separate modules at `contrib/decoders/json`,
`contrib/decoders/yaml` and `contrib/decoders/toml`. Each exports `Decode` with the
`xconf.Decoder` signature. The `sources/json`, `sources/yaml` and `sources/toml`
modules retain convenient `File` constructors and forward `Decode` to these decoders.
For example, `http.New(client, url, json.Decode)` and
`s3.New(client, bucket, key, yaml.Decode)` use the same schema pipeline as local files.
Source adapters share bounded reads and location/revision metadata handling through
`internal/document`, an internal package of the root module. The public decoder
contract remains `xconf.Decoder`; each source and decoder integration has its own
`contrib` module.

See [source integration details](docs/sources.md) for construction, authentication,
reload, missing-data behavior and projected-volume semantics.

Files poll every 250ms, including rename/recreation and recovery after invalid reads.
`file.Interval(0)` disables watching; `file.MaxBytes` changes the default 8MiB limit.
`Optional` ignores only `ErrNotFound`, never invalid syntax, permissions or outages.
`Poll(source, interval)` adds polling to any source. Static env/flags change only on
manual reload or polling; they do not install process-global watchers.

Env names derive from schema paths (`db.host` -> `APP_DB_HOST`). `env.Bind` maps an
exact name to a path; `env.Split` enables explicit list delimiters, otherwise
containers use JSON syntax. Empty scalar strings are preserved; empty split lists
are empty lists. `env.Strict()` rejects unknown names under a nonempty prefix.
Ambiguous names and overlapping parent/child inputs are errors. If the same path
has scalar and container variants in OneOf, supply the whole variant as JSON. YAML merge keys and
non-string mapping keys are rejected. Flags require explicit values, including bools;
repeated flags use the last value. No implicit file search or CLI globals. Dotenv interpolation follows godotenv
syntax inside that document; it does not expand against or modify process env.

`Source.Read(ctx, schema)` returns a whole Layer, including optional location/revision
metadata and explicit edits. It receives a schema copy and must return owned data.
`Watcher.Watch` synchronously registers invalidations and returns a cleanup function.
`SourceFunc` adapts custom readers; `NewMemory` supplies concurrent test/override data.

Source error strings omit provider payloads; explicit Unwrap exposes the original
cause for applications that need it. Raw Baked and validation diagnostics are explicit
accessors and may contain secrets: do not log them as a configuration dump.

## Development and release

Go 1.25.7 or newer. `make test` builds, vets and race-tests every module with
`GOWORK=off`. Contrib modules use local replacements inside this checkout; consumers
use published module versions. `make release` keeps root and contrib tags aligned
within v1. It does not migrate or upgrade downstream applications.
