# Runtime and custom sources

## Publication and subscriptions

`Open` registers watchers before reading the initial configuration. `OpenAs[T]`
also checks typed decoding before publishing. Initial failure aborts construction.
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
