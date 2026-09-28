# Runtime and custom sources

## Publication and subscriptions

`Open` registers watchers before reading the initial configuration. `OpenAs[T]`
also checks typed decoding before publishing. Initial failure aborts construction,
unless a degraded source may be its cause (see below).
A later read, merge, validation or decode failure retains the last valid snapshot
and emits an error event containing it.

Reloads rebuild complete source layers and are serialized. Watch notifications
coalesce over 20 ms; changes during a rebuild schedule another pass. `Reload(ctx)`
requests a rebuild directly.

Each subscriber receives the current snapshot immediately and retains only its
latest event. Slow subscribers may skip versions. `Event.Changed` compares
consecutive published snapshots, so consumers that skip versions must reconcile
full state. Cancel a subscription's context to unsubscribe.

Equal values and provenance retain their version. Source revision or provenance
changes can advance it even if values are equal. Recovery from a failed reload
emits a successful event even when the version remains unchanged.

Snapshot accessors and typed decoding return independent data. Custom decode hooks
should be deterministic. `Close` cancels reads, stops watchers and waits for shutdown;
sources must honor context. Publication is process-local, without transactions
across remote stores. Application component reconfiguration remains caller-owned.

## Unavailable remote sources

`xconf.Resilient(source)` keeps a remote source such as Consul in effect while it
is down, at startup or later. Any `Read` error returns the last good layer (empty
before the first success) with `Layer.Stale` set to the error, so `Open` and
reloads do not fail because of that source. `snapshot.Degraded()` maps each stale
source name to its raw error; it is empty when all sources are healthy. The raw
errors may contain secrets, and `SourceError` text still omits them.

A source becoming stale or recovering publishes a normal event; a different error
from a source that is already stale does not. While stale, the wrapper invalidates
the runtime with exponential backoff (`xconf.Backoff(min, max)`, default 1s..30s)
until a read succeeds. A failing `Watch` registration of the inner source is
retried in the background and followed by an invalidation. All retries stop with
`Close`. `ErrNotFound` counts as a failure; compose
`xconf.Resilient(xconf.Optional(source))` to treat a missing key as empty instead.

A required value served only by a degraded source holds `Open` until the source
recovers or `ctx` ends — use a `ctx` deadline to bound startup. Precisely: when the
initial validation (or `OpenAs` typed decoding) fails while any source is degraded,
`Open` and `OpenAs` keep the watchers registered and retry the initial load on every
notification, including the wrapper's backoff retries, until it succeeds or `ctx`
ends. On `ctx` end they stop all watchers and return the last failure joined with
`ctx.Err()`. A failure with no degraded source still aborts immediately. `Load` and
`LoadAs` never wait. The `Open` context is also the runtime's lifetime, so bound
only startup with a timer that cancels it and is stopped once `Open` returns.

Such failures, including failed reloads, are `*xconf.DegradedError`: `Degraded`
holds the raw source errors (may contain secrets), `Unwrap` yields the underlying
`*xconf.ValidationError` or decode error, and `Error()` adds only the source names.

```go
ctx, cancel := context.WithCancel(ctx) // runtime lifetime
startup := time.AfterFunc(time.Minute, cancel) // bounds a held Open
runtime, err := xconf.Open(ctx, schema, defaults,
    xconf.Resilient(consul.NewPrefix(client.KV(), "apps/api")))
startup.Stop()
// ...
for name, err := range runtime.Snapshot().Degraded() {
    log.Printf("config source %s degraded: %v", name, err)
}
```

## Provenance

`snapshot.Origins()` returns `map[string][]xconf.Origin` for all recorded paths,
including overridden source writes and schema operations such as defaults and
coercion. Each key is a JSON pointer: `/server/port`, or `/labels/a~1b` for the
literal map key `a/b`. Each history has the same order as `Explain(path...)`.
Paths represent recorded operations, not just fields remaining in the final value.

The returned map and every history slice are independent copies; changing them
does not modify the snapshot. An empty snapshot returns an empty, non-nil map.
Origins contain source, revision, operation and location metadata, without values.
Use this accessor to collect all provenance in one pass instead of traversing the
configuration and calling `Explain` for each field.

## Custom sources

Implement these interfaces from the root package:

```go
type Source interface {
    Name() string
    Read(context.Context, *sp.Schema) (xconf.Layer, error)
}

type Watcher interface {
    Watch(context.Context, func()) (func(), error)
}
```

Use a stable name unique within the loader. `Read` receives a schema copy and must
return a complete, independently owned partial layer on each call. It must not
apply defaults or require a complete configuration. `Layer.Edits` run in order
after `Layer.Values`; location keys are JSON pointers.

`SourceFunc` adapts custom readers:

```go
source := xconf.SourceFunc{
    ID: "application-overrides",
    ReadFunc: func(ctx context.Context, schema *sp.Schema) (xconf.Layer, error) {
        if err := ctx.Err(); err != nil {
            return xconf.Layer{}, err
        }
        return xconf.Layer{
            Values: map[string]any{"port": int64(7000)},
        }, nil
    },
}
```

`Watcher.Watch` registers invalidations synchronously and returns cleanup logic.
Alternatively, wrap a source in `xconf.Poll(source, interval)`. `NewMemory` supplies
a concurrent mutable source for tests and application overrides. `Optional` only
suppresses `ErrNotFound`; do not classify outages or malformed data as missing.
