# xconf

Load configuration from explicit sources, merge them in order, and validate the
result against a [schemapb](https://github.com/gopherex/schemapb) schema. Keep a
validated snapshot or subscribe to changes while the application is running.

```text
config file → local overrides → environment → flags
                         ↓
                  merge raw values
                         ↓
             schemapb defaults and validation
                         ↓
              typed config / live snapshot
```

You choose the sources and their priority. xconf handles loading, merging,
validation, reloads and provenance. Your application decides how to apply a new
configuration to its components.

- Define a schema from a Go struct or supply an existing `*schemapb.Schema`.
- Combine files, environment variables, flags and remote stores.
- Reject invalid updates while keeping the last valid configuration.
- Inspect which source or schema operation supplied a field.
- Import only the source and decoder modules you need.

## Requirements and availability

Go **1.25.7 or newer**. The module path is `github.com/gopherex/xconf`.
Each integration under `contrib/` has its own `go.mod` and release tag.

**This README describes the rewritten API on `master`.** The published `v1.1.2`
release contains the previous API; the new contrib modules are not yet released.
Until the next coordinated v1 release, use the checkout example below. When adding
released integrations to an application, select matching root and contrib versions.

## Try it

```sh
git clone https://github.com/gopherex/xconf.git
cd xconf/example
GOWORK=off go run . -once
```

The [example](example/main.go) reads [config.json](example/config.json), then applies
variables prefixed with `APP_`:

```sh
APP_SERVER_PORT=7000 GOWORK=off go run . -once
```

This prints `version=1 server=127.0.0.1:7000`. Run without `-once` and edit
`config.json` to see live updates. An environment override keeps its priority over
subsequent file changes. Press Ctrl+C to stop.

## Load a typed configuration

Given `config.json`:

```json
{
  "server": {
    "port": 9090
  }
}
```

This complete program reads the file, overlays the environment, applies defaults
and validates the resulting configuration:

```go
package main

import (
    "context"
    "fmt"
    "log"

    sp "github.com/gopherex/schemapb/go/schemapb"
    "github.com/gopherex/xconf"
    "github.com/gopherex/xconf/contrib/sources/env"
    jsonconf "github.com/gopherex/xconf/contrib/sources/json"
)

type Config struct {
    Server struct {
        Host string `json:"host" schemapb:"default=127.0.0.1"`
        Port int64  `json:"port" schemapb:"default=8080;gte=1;lte=65535"`
    } `json:"server" schemapb:"default={}"`
}

func main() {
    schema, err := sp.ReflectType[Config](sp.ID("app", "config", sp.Ver(1, 0, 0)))
    if err != nil {
        log.Fatal(err)
    }

    cfg, err := xconf.LoadAs[Config](context.Background(), schema,
        jsonconf.File("config.json"),
        xconf.Optional(jsonconf.File("config.local.json")),
        env.New(env.Prefix("APP_")),
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("%s:%d\n", cfg.Server.Host, cfg.Server.Port)
}
```

The result is `127.0.0.1:9090`. With `APP_SERVER_PORT=7000`, it becomes
`127.0.0.1:7000`. An out-of-range port fails validation.

`default={}` makes an absent `server` section exist so its child defaults can run.
Reflection enables coercion, allowing an environment string such as `"7000"` to
become an integer. If you construct a schema directly, enable coercion for textual
inputs. Defaults, formats, computed fields and validation rules belong to schemapb.

`Optional` suppresses only `xconf.ErrNotFound`. A missing local override is fine;
invalid syntax, permission errors and unavailable remote stores still fail loading.
xconf does not search for configuration files or read CLI arguments implicitly.

## Source priority and merging

Sources are ordered **lowest priority first, highest priority last**. Each source
supplies a partial layer. Defaults and validation run once, after all layers merge.

| Later input | Effect |
| --- | --- |
| Object or map | Recursively merges keys with the earlier object |
| Scalar or list | Replaces the earlier value; lists are not appended |
| `0`, `false`, `""`, `[]` | Overrides the earlier value explicitly |
| `null` | Replaces the earlier value; schema nullability still applies |
| `{}` | Preserves earlier object keys |
| `Edit{Kind: xconf.Replace}` | Replaces a whole object, including with `{}` |
| `Edit{Kind: xconf.Delete}` | Removes a key from merged input; a schema default may restore it |

Changing a OneOf discriminator replaces the previous variant's whole object.
Unknown fields follow the schema's strictness setting. Null is never interpreted
as a missing value.

Every reload reads complete source layers again. If a higher-priority source stops
providing a field, a lower-priority value or schema default becomes visible.

## Live reload and subscriptions

With the same `Config` and schema, use `OpenAs` instead of `LoadAs`. The following
function uses the imports from the loading example:

```go
func watch(ctx context.Context, schema *sp.Schema) error {
    runtime, err := xconf.OpenAs[Config](ctx, schema,
        jsonconf.File("config.json"),
        env.New(env.Prefix("APP_")),
    )
    if err != nil {
        return err
    }
    defer runtime.Close()

    for event := range runtime.Subscribe(ctx) {
        if event.Err != nil {
            log.Printf("configuration update rejected: %v", event.Err)
            continue
        }
        cfg, err := xconf.Decode[Config](event.Snapshot)
        if err != nil {
            return err
        }
        fmt.Printf("version=%d server=%s:%d\n",
            event.Snapshot.Version(), cfg.Server.Host, cfg.Server.Port)
        // Apply cfg to the application's components here.
    }
    return nil
}
```

Pass a cancellable context, such as `signal.NotifyContext`, to stop the loop.
`Subscribe` immediately delivers the current snapshot, then subsequent updates.
`runtime.Current()` returns the current typed value; `runtime.Reload(ctx)` requests
an immediate rebuild. `Close` cancels reads and stops watchers.

- An initial read, validation or typed-decoding failure makes `OpenAs` fail.
- A failed reload preserves the last valid snapshot and emits an error event.
- Watchers register before the initial read. Reloads are serialized, with watch
  notifications coalesced over a 20 ms window.
- Slow subscribers receive the latest event, dropping intermediate events.
  Reconcile full state: `event.Changed` compares consecutive published snapshots,
  not necessarily the snapshots your subscriber received.
- Unchanged values and provenance keep the same version. Source revision or
  provenance changes can advance it even when values remain identical. Recovery
  from a failed reload emits a successful event even if the version is unchanged.

Snapshot accessors and typed decoding return independent data. Publication is
atomic inside the process; reads across separate remote stores are not a transaction.
The application owns connection pools, listeners and other reconfiguration work.

## Available sources

Import paths below start with `github.com/gopherex/xconf/contrib/sources/`.
Each row is an independent Go module.

| Module | Input | Automatic updates |
| --- | --- | --- |
| `json` | JSON file via `File(path)` | File polling |
| `yaml` | YAML file via `File(path)` | File polling |
| `toml` | TOML file via `File(path)` | File polling |
| `file` | File with a supplied decoder | File polling |
| `fs` | Any `fs.FS`, including embedded files | Use `xconf.Poll` for mutable filesystems |
| `reader` | Fresh reader factory, or one captured stream | Manual reload or `xconf.Poll` |
| `env` | Process or injected environment | Manual reload or `xconf.Poll` |
| `dotenv` | `.env` file without modifying process environment | File polling |
| `envfile` | Explicit `*_FILE` variables referencing secret files | File polling |
| `directory` | One field per file, including mounted secrets | Directory polling |
| `flags` | Explicit argument slice and field bindings | Fixed arguments |
| `pflag` | Parsed pflag/Cobra flags marked `Changed` | Captured at construction |
| `http` | HTTP document with ETag / Last-Modified support | Conditional polling |
| `consul` | KV document or prefix mapped to fields | Blocking queries |
| `etcd` | Document in an etcd v3 key | Revision watch and reconciliation |
| `kubernetes` | ConfigMap/Secret fields or document | API watch and reconciliation |
| `nats` | Document in a JetStream KV key | KV watch and reconciliation |
| `s3` | S3-compatible object, optionally pinned to a version | Polling |
| `vault` | Vault KV v2 data, optionally pinned to a version | Polling |

File polling defaults to 250 ms. HTTP, S3 and Vault poll every 30 seconds.
`xconf.Poll(source, interval)` adds periodic invalidation to a source. Source-specific
options control timeouts, size limits, names and update intervals.

Environment names follow schema paths: `server.port` becomes `APP_SERVER_PORT` with
`env.Prefix("APP_")`. Use `env.Bind` for explicit names. Containers use JSON syntax
unless you explicitly configure `env.Split`; scalar empty strings remain present.
`env.Strict()` rejects unknown names under a nonempty prefix.

Remote adapters accept caller-owned clients. Configure credentials, TLS and
endpoints on those clients; closing a runtime does not close them. Vault token
renewal remains the application's responsibility.

See [source integration details](docs/sources.md) for constructors, client setup,
watch recovery, missing-data behavior and Kubernetes permissions.

## Decoders

A decoder turns document bytes into a raw object and optional locations:

```go
type Decoder func([]byte) (map[string]any, map[string]xconf.Location, error)
```

| Module under `contrib/decoders/` | Format rules |
| --- | --- |
| `json` | One JSON object; preserves numeric tokens without a float64 intermediate |
| `yaml` | One mapping; bounded alias expansion, source locations, no merge keys or non-string keys |
| `toml` | TOML document; preserves date and time values |

Each exports `Decode` implementing `xconf.Decoder`. Choose the transport and format
independently. For example, using these aliases:

```go
import (
    "net/http"

    jsondecode "github.com/gopherex/xconf/contrib/decoders/json"
    httpconf "github.com/gopherex/xconf/contrib/sources/http"
)

var remote = httpconf.New(http.DefaultClient,
    "https://config.example/app.json", jsondecode.Decode)
```

The `sources/json`, `sources/yaml` and `sources/toml` modules provide convenient
file constructors and also forward `Decode`. Document decoding does not apply
schema defaults or validation. Shared bounded-reading and metadata helpers live
in the root module's `internal/document`; there is no public document module.

## Snapshots and diagnostics

| API | Result |
| --- | --- |
| `LoadAs[T](ctx, schema, sources...)` | Validated typed configuration |
| `Load(ctx, schema, sources...)` | Snapshot with diagnostics and provenance |
| `Decode[T](snapshot)` | Independently owned typed value |
| `OpenAs[T](ctx, schema, sources...)` | Live runtime that checks typed decoding before publication |
| `Open(ctx, schema, sources...)` | Live runtime without an application struct |
| `New(schema, compileOptions...)` | Reusable loader with schemapb compile options |

Use `snapshot.Explain("server", "port")` to inspect the source writes and schema
operations that contributed to a field. Paths are slices of segments and stringify
as JSON pointers, so map keys containing dots or slashes remain unambiguous.
Explanation records contain provenance, not configuration values.

Source error messages omit provider payloads. Unwrapping errors or inspecting
`Baked()`, `Report()` and `Validation()` may expose configuration data; avoid logging
these as a dump when the configuration contains secrets.

## Custom sources

Implement `Source` or use `SourceFunc`. A source returns a complete, independently
owned **partial layer** on each read. It must honor cancellation, use a stable name
unique within the loader, and leave schema defaults and validation to xconf.

```go
var overrides = xconf.SourceFunc{
    ID: "application-overrides",
    ReadFunc: func(ctx context.Context, schema *sp.Schema) (xconf.Layer, error) {
        if err := ctx.Err(); err != nil {
            return xconf.Layer{}, err
        }
        return xconf.Layer{
            Values: map[string]any{
                "server": map[string]any{"port": int64(7000)},
            },
        }, nil
    },
}
```

`Layer` can also contain a revision, locations keyed by JSON pointer, and ordered
`Edits` applied after `Values`. For live sources, implement `Watcher.Watch` to
register invalidations synchronously and return a cleanup function, or wrap the
source in `xconf.Poll`. `NewMemory` provides a concurrent mutable source for tests
and application-controlled overrides.

## Development

```sh
make test  # build, vet and race-test every module, including the example
make tidy  # tidy dependencies in every module
```

Contrib modules use local `replace` directives so development and CI exercise the
current checkout with `GOWORK=off`. External consumers use the versions declared
in `require`. `make release` aligns root and peer requirements and creates a root
tag plus a tag for each contrib module. The example is excluded from release tags.
A checkout build does not replace a consumer check against published versions.

## Migrating from the previous API

This rewrite stays on **v1**, with the same root import path, and deliberately
breaks the previous API. Existing applications can keep their pinned releases
until they migrate.

The former xconf DSL, `xconfgen`, `pkg/load` and `pkg/structconf` are removed. Move
schema definitions and validation rules to schemapb, select explicit contrib
sources, and use `LoadAs` or `OpenAs` for typed configuration. Upgrading the library
does not automatically migrate consumers.
