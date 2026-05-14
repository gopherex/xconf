# xconf

Typed, declarative configuration for Go. Fluent schema DSL, automatic env
naming, runtime loading from env/JSON/YAML/TOML, and `go generate`-based
codegen of a typed `Config` struct + a zero-reflection `LoadFromEnv` entry
point.

## Why

- **Type-safe at definition time.** `xconf.Int("Port").Validate(validate.Range(1, 65535))`
  rejects type mismatches at compile time via generics.
- **Composable.** External libraries export their own `*Schema`; consumers
  `Embed("Redis", redislib.ConfigSchema)` to scope it under any name. No
  duplication of struct shapes.
- **Multi-source.** Defaults < JSON/YAML/TOML files < env vars, with the last
  source winning. Sources are a small interface — bring your own.
- **Two loading paths.**
  - `Load(sources ...load.Source)` — reflection-based, plugs in any source.
  - `LoadFromEnv()` — generated, zero-reflection on the hot path.
- **Two authoring paths.**
  - Fluent DSL (`xconf.Define(...)`) — primary, most expressive.
  - Struct tags (`pkg/structtag`) — derive a schema from `xconf:"..."` tags
    on an existing struct.

## Install

```bash
go get github.com/gopherex/xconf@latest
go install github.com/gopherex/xconf/cmd/xconfgen@latest
```

`go get` pulls the library. `go install` puts the `xconfgen` binary on your
`$PATH` so `//go:generate xconfgen ...` works. Make sure `$(go env GOBIN)`
(or `$(go env GOPATH)/bin` if `GOBIN` is empty) is in your `PATH`.

Optional sub-packages are pulled transitively when imported:

```go
import (
    "github.com/gopherex/xconf"
    "github.com/gopherex/xconf/pkg/validate"
    "github.com/gopherex/xconf/pkg/load"
    "github.com/gopherex/xconf/pkg/structtag" // only if you use struct tags
)
```

## Quick start

Full copy-paste flow from zero to a working typed config:

```bash
mkdir myapp && cd myapp
go mod init example.com/myapp
go get github.com/gopherex/xconf@latest
go install github.com/gopherex/xconf/cmd/xconfgen@latest
```

Create `config/schema.go`:

```go
package config

import (
    "github.com/gopherex/xconf"
    "github.com/gopherex/xconf/pkg/validate"
)

//go:generate xconfgen -type AppConfig

var Schema = xconf.Define("AppConfig",
    xconf.Int("Port").Default(8080).Validate(validate.Range(1, 65535)),
    xconf.String("DSN").Env("DB_DSN").Required(),
)
```

Create `main.go`:

```go
package main

import (
    "fmt"

    "example.com/myapp/config"
    "github.com/gopherex/xconf/pkg/load"
)

func main() {
    cfg, err := config.LoadFromEnv()
    if err != nil { panic(err) }
    fmt.Printf("%+v\n", cfg)
    _ = load.FromEnv // referenced for the Load() variant
}
```

Generate and run:

```bash
go generate ./...
DB_DSN=postgres://localhost/x go run .
```

### Schema with composition and validation

```go
package app

import (
    "github.com/gopherex/xconf"
    "github.com/gopherex/xconf/example/redislib"
    "github.com/gopherex/xconf/pkg/validate"
)

//go:generate xconfgen -type AppConfig

var Schema = xconf.Define("AppConfig",
    xconf.Int("Port").
        Default(8080).
        Validate(validate.Range(1, 65535)),

    xconf.String("DSN").
        Env("DB_DSN").
        Required().
        Validate(validate.NonEmpty()),

    xconf.Slice[string]("AllowedHosts").
        EnvSplit(",").
        Validate(validate.Each(validate.NonEmpty())),

    xconf.Map[string, int]("RateLimits").
        EnvSplit(",").KVSplit("="),

    xconf.Embed("Redis", redislib.ConfigSchema),
)
```

Run `go generate ./...` to produce `appconfig_gen.go` containing
`type AppConfig struct { ... }`, `Load(sources ...) (*AppConfig, error)`,
and `LoadFromEnv() (*AppConfig, error)`.

Use it:

```go
import (
    "github.com/gopherex/gopherex/xconf/example/app"
    "github.com/gopherex/xconf/pkg/load"
)

func main() {
    cfg, err := app.Load(
        must(load.FromYAMLFileOptional("config.yaml")),
        load.FromEnv(nil), // env wins
    )
    if err != nil { panic(err) }
    _ = cfg.Port
}
```

Or, for the no-reflection hot path:

```go
cfg, err := app.LoadFromEnv() // env-only, typed parsing
```

## Authoring schemas

### Fluent DSL

| Constructor | Field type |
|------------|------------|
| `Int`, `Int8/16/32/64` | signed integers |
| `Uint`, `Uint8/16/32/64` | unsigned integers |
| `Float32`, `Float64` | floats |
| `String`, `Bytes` | string, []byte |
| `Bool` | bool |
| `Duration`, `Time` | time.Duration, time.Time |
| `Slice[T]` | []T |
| `Map[K, V]` | map[K]V |
| `Group(name, ...)` | inline nested struct |
| `GroupAs[T](name, ...)` | nested group bound to existing Go type T |
| `WithLoader(s, fn)` | attach external loader to a group |
| `Embed(name, sub)` | re-root an external schema under a new name |

Chain methods on `*Field[T]`:

```
.Default(v) .Env(name) .Required() .Description(s) .Validate(v)
```

`*SliceField[T]` adds `.EnvSplit(sep)`. `*MapField[K,V]` adds `.EnvSplit`,
`.KVSplit`. `*Schema` adds `.EnvPrefix(p)`.

### Automatic env names

Auto-derived as `<GROUP_PREFIX>_<FIELD_NAME>` in SCREAMING_SNAKE_CASE.

- Root `Define` contributes no prefix by default.
- Nested `Group("DB")` adds `DB_` to descendants.
- `Embed("Redis", sub)` rescopes `sub` under `REDIS_`.
- Explicit `.Env("X")` wins.

`HTTPServer` → `HTTP_SERVER`. `AllowedHosts` → `ALLOWED_HOSTS`.

### External library composition

Libraries export a schema **and** a Go type. Consumers don't redeclare either:

```go
// redislib/redislib.go
type Config struct {
    Addr    string
    Timeout time.Duration
}
var ConfigSchema = xconf.GroupAs[Config]("Config",
    xconf.String("Addr").Default("localhost:6379"),
    xconf.Duration("Timeout").Default(5*time.Second),
)
```

```go
// app/schema.go
var Schema = xconf.Define("AppConfig",
    xconf.Embed("Redis", redislib.ConfigSchema),
)
// generated AppConfig has: Redis redislib.Config
```

Add `WithLoader` to delegate loading of that subtree to the library's own
loader:

```go
var ConfigSchema = xconf.WithLoader(
    xconf.GroupAs[Config]("Config", ...),
    LoadConfig, // func() (*Config, error)
)
```

The loader's fully-qualified name is captured via `runtime.FuncForPC`; both
the codegen path (`Load`, `LoadFromEnv`) and the reflective `load.Load`
delegate to it.

### Struct tags (alternative)

For code-first projects, derive a schema from struct tags:

```go
type AppCfg struct {
    Port    int           `xconf:"default=8080"`
    DSN     string        `xconf:"env=DB_DSN,required"`
    Tags    []string      `xconf:"split=|"`
    Limits  map[string]int `xconf:"split=;,kv=:"`
    Timeout time.Duration `xconf:"default=2s"`
    DB      DBCfg          // nested struct → Group
    Skipped string        `xconf:"skip"`
}

schema, _ := structtag.SchemaFromStruct[AppCfg]("App")
```

Supported keys: `env`, `default`, `required`, `desc`, `split`, `kv`, `skip`.
Validators are not expressible in tags (they're typed closures) — compose
with the fluent API if needed.

## Sources

```go
load.FromEnv(nil)                    // os.Getenv
load.FromEnv(map[string]string{...}) // injected env (tests)
load.FromMap(map[string]any{...})    // nested map
load.FromJSONFile("c.json")
load.FromYAMLFile("c.yaml")
load.FromTOMLFile("c.toml")
load.FromJSONFileOptional(...)       // no error if missing
```

Sources are passed in priority order; later wins. Defaults apply if no
source provides a value. `.Required()` fields error when no value is found.

Implement your own:

```go
type Source interface {
    Lookup(d xconf.FieldDesc, path []string) (raw any, ok bool, err error)
}
```

## Validators (`pkg/validate`)

Typed via generics. Mismatched T fails at compile time.

- Numeric: `Range`, `Min`, `Max`, `Positive`, `NonNegative`, `NonZero`
- Equality: `OneOf`, `Equal`
- String: `NonEmpty`, `MinLen`, `MaxLen`, `LenBetween`, `Regex`,
  `HasPrefix`, `HasSuffix`, `Contains`, `URL`, `Email`
- Slice: `MinItems`, `MaxItems`, `Unique`, `Each`
- Map: `MapMinSize`, `MapMaxSize`, `MapHasKey`, `MapKeys`, `MapValues`
- Combinators: `All`, `Any`, `Not`

## Codegen

`xconfgen` is installed once (`go install ./cmd/xconfgen`) and invoked via
`go:generate`:

```go
//go:generate xconfgen -type AppConfig
```

Flags:

- `-pkg` — schema package path (default `.`)
- `-var` — schema variable name (default `Schema`)
- `-type` — root struct name (required)
- `-out` — output file (default `<lower(type)>_gen.go`)
- `-loadfn` — generated load function name (default `Load`)

What gets emitted:

- One struct per inline `Group` (root + nested non-bound)
- `BindType` groups reuse the external type (no duplicate struct)
- `BindLoader` groups: `cfg.X = *LoaderFn()` after env/source pass
- `Load(sources ...load.Source) (*T, error)` — runtime path
- `LoadFromEnv() (*T, error)` — typed env parsing inline, then validators
  via `load.Validate`

## Layout

```
xconf/
  xconf.go                       public facade (type aliases, constructors)
  cmd/xconfgen/                  CLI for go:generate
  internal/core/                 implementation
  pkg/
    validate/                    typed validators
    load/                        runtime sources + loader
    codegen/                     Render(*Schema) → Go source
    structtag/                   schema-from-struct-tags
  example/
    redislib/                    external-library schema example
    app/                         consumer schema + generated file
```
