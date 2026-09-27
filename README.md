# xconf

Load configuration from explicit sources, merge them in priority order, and apply a
[schemapb](https://github.com/gopherex/schemapb) schema. Supports typed values, live
reload, subscriptions and field provenance. Invalid updates keep the last valid config.

**Go 1.25.7+.** Root module: `github.com/gopherex/xconf`. Sources and decoders are
separate contrib modules. This README describes `master`; published `v1.1.2` still
contains the previous API. The rewritten API stays on v1 and requires migration.

## Quick start

Run the checkout example:

```sh
git clone https://github.com/gopherex/xconf.git
cd xconf/example
APP_SERVER_PORT=7000 GOWORK=off go run . -once
```

Output: `version=1 server=127.0.0.1:7000`. Omit `-once` and edit `config.json` for
live reload. The environment override keeps priority over the file.

A complete application reading `config.json`, an optional local file and environment:

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

With `{"server":{"port":9090}}`, the result is `127.0.0.1:9090`.
`APP_SERVER_PORT=7000` overrides the port; `APP_SERVER_PORT=70000` fails validation.
An existing `*schemapb.Schema` can be passed directly instead of reflecting a struct.

## Struct tags

`json` names fields; `schemapb` defines defaults and constraints. Separate attributes
with `;`. Reflection builds the schema; defaults and constraints apply after merging.

| Tag | Meaning |
| --- | --- |
| `json:"port"` | Key in documents; becomes `APP_PORT` with the `APP_` env prefix |
| `json:"port,omitempty"` | Makes an absent field optional; explicit zero remains present |
| `json:"-"` | Excludes the field |
| `schemapb:"default=8080;gte=1;lte=65535"` | Integer default and inclusive bounds |
| `schemapb:"default=30s;gt=0s"` | Duration default and positive constraint |
| `schemapb:"default=false"` | Boolean default; explicit `false` is preserved |
| `schemapb:"required=true;min_len=1"` | Requires a string field and rejects an empty string |
| `schemapb:"default={}"` | Creates a missing object so child defaults can apply |
| `schemapb:"min_items=1;max_items=8"` | List size bounds |
| `schemapb:"in=[\"fast\",\"safe\"];default=safe"` | Allowed string values and default |
| `schemapb:"secret=true"` | Marks schema metadata as secret; does not encrypt or erase the value |
| `schemapb:"description=API address"` | Field description |

Non-pointer fields without `omitempty` are required by default. Pointers are
optional and nullable; `required=true` and `nullable=false` can override those
properties independently. Defaults apply to missing keys, never explicit null,
zero, false or empty strings. List/map defaults and nonempty object defaults are
not supported by the pinned schemapb version.

The supported `validate` vocabulary includes `required`, numeric comparisons,
string lengths, list `min`/`max`, `oneof`, and formats such as `email`, `url` and
`uuid`. It is not the full go-playground validator. Use schemapb attributes for
explicit schema rules. See [tag examples and limitations](docs/tags.md).

## Priority and merge rules

Pass sources from **lowest to highest priority**. Objects/maps merge recursively;
scalars, null and lists replace previous values. Zero, false, `""` and `[]` are
explicit overrides. `{}` preserves lower object keys; use a `Replace` edit to clear
an object. `Delete` removes a key before schema defaults run. Changing a OneOf
discriminator replaces the previous variant's object.

`Optional` suppresses only `ErrNotFound`, not malformed input or unavailable stores.
Removing an override reveals lower layers or defaults on the next successful reload.
Unknown fields follow schema strictness. Reflection enables coercion for textual
sources; enable it explicitly in hand-built schemas.

## Reload and subscriptions

Using the imports and `Config` above:

```go
func watch(ctx context.Context, schema *sp.Schema) error {
    runtime, err := xconf.OpenAs[Config](ctx, schema,
        jsonconf.File("config.json"), env.New(env.Prefix("APP_")),
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
        fmt.Printf("server=%s:%d\n", cfg.Server.Host, cfg.Server.Port)
        // Reconfigure application components here.
    }
    return nil
}
```

Cancel `ctx` to stop. Subscribers receive the current snapshot immediately.
`Current()` returns an independent typed value; `Reload(ctx)` requests a rebuild.
Initial failure aborts `OpenAs`; later failures retain the last valid snapshot.
Slow subscribers receive only the latest event, so reconcile full state.
See [runtime semantics and custom sources](docs/runtime.md).

## Sources: where to load

Module paths start with `github.com/gopherex/xconf/contrib/sources/`.
Document sources accept a decoder; environment and field-oriented sources supply
values directly.

| Module | Input | Updates |
| --- | --- | --- |
| `file` | File with a supplied decoder | Polling |
| `fs` | `fs.FS`, including embedded files | Manual / `xconf.Poll` |
| `reader` | Reader factory or captured stream | Manual / `xconf.Poll` |
| `env` | Process or injected environment | Manual / `xconf.Poll` |
| `dotenv` | `.env` file, without modifying process env | Polling |
| `envfile` | Explicit `*_FILE` variables pointing to files | Polling |
| `directory` | One field per file, including mounted secrets | Polling |
| `flags` | Explicit arguments and field bindings | Fixed arguments |
| `pflag` | Changed pflag/Cobra flags | Captured after parsing |
| `http` | HTTP document | Conditional polling |
| `consul` | KV document or prefix mapped to fields | Blocking queries |
| `etcd` | Document in an etcd v3 key | Watch |
| `kubernetes` | ConfigMap/Secret fields or document | Watch |
| `nats` | JetStream KV document | Watch |
| `s3` | S3-compatible object | Polling |
| `vault` | Vault KV v2 data | Polling |

Environment paths map to names such as `server.port` → `APP_SERVER_PORT`.
`env.Bind` supplies explicit mappings; `env.Strict()` rejects unknown prefixed
names. Containers use JSON unless `env.Split` is configured. Remote clients,
authentication and TLS belong to the caller. See [source options](docs/sources.md).

## Decoders: how to parse

Module paths start with `github.com/gopherex/xconf/contrib/decoders/`.
Each `Decode` implements `xconf.Decoder` and performs no I/O or schema validation.

| Module | Format |
| --- | --- |
| `json` | One JSON object; exact numeric tokens |
| `yaml` | One mapping; bounded aliases and source locations; no merge keys |
| `toml` | TOML document, including date/time values |

For example, `httpconf.New(client, url, jsondecode.Decode)` combines an HTTP source
with a JSON decoder. `sources/json`, `sources/yaml` and `sources/toml` are file
convenience wrappers: `jsonconf.File(path)` combines `file` with the JSON decoder.
The shared `internal/document` helper is private to the root module.

## API and development

| API | Result |
| --- | --- |
| `LoadAs[T](ctx, schema, sources...)` | Typed config |
| `Load(ctx, schema, sources...)` | Snapshot with diagnostics and provenance |
| `Decode[T](snapshot)` | Independently owned typed value |
| `OpenAs[T](ctx, schema, sources...)` | Runtime with typed checks before publication |
| `Open(ctx, schema, sources...)` | Untyped runtime |
| `New(schema, compileOptions...)` | Reusable compiled loader |

`snapshot.Explain("server", "port")` returns provenance without values. Raw snapshot
diagnostics and unwrapped provider errors may contain secrets.

`make test` builds, vets and race-tests every module. `make tidy` updates dependencies.
Local contrib `replace` directives support checkout development with `GOWORK=off`;
consumers use published versions. `make release` aligns root/contrib requirements
and tags. Verify a released consumer separately, without local replacements.

The old xconf DSL, `xconfgen`, `pkg/load` and `pkg/structconf` are removed. Migrate
schemas to schemapb and loading to `LoadAs` / `OpenAs`; existing applications can
keep their pinned releases until migrated.
