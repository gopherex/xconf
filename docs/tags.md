# Struct tags

xconf passes the merged input to schemapb. `sp.ReflectType[Config](id)` derives the
schema from Go types and tags; the same rules apply to every source.

## Names and presence

| Declaration | Behavior |
| --- | --- |
| `Port int64` | Field name `Port`; required |
| `Port int64` with `json:"port"` | Field name `port`; required |
| `Port int64` with `json:"port,omitempty"` | Optional; missing decodes to zero unless defaulted |
| `Port *int64` with `json:"port"` | Optional and nullable; missing/null decodes to nil |
| `json:"-"` | Field excluded from the schema |
| Untagged embedded struct | Exported fields flattened into the parent |

`omitempty` changes schema requiredness; it does not discard explicit zero, false
or empty values from a source. `required=true` requires presence; use value
constraints such as `min_len=1` or `gt=0` to reject empty strings or zero.
Requiredness and nullability are separate: a required nullable field may be null.
`schemapb:"required=false"` and `schemapb:"nullable=false"` override inference.
Defaults are resolved before requiredness is checked.

Use identifier-like JSON names (`server_port`, not `server-port`); CEL reserved
words are rejected. Without a JSON name, the Go field's spelling is used.

## Defaults and constraints

```go
type Settings struct {
    Token   string        `json:"token" schemapb:"required=true;min_len=1;secret=true"`
    Workers int32         `json:"workers" schemapb:"default=4;gte=1;lte=64"`
    Timeout time.Duration `json:"timeout" schemapb:"default=30s;gt=0s"`
    Debug   bool          `json:"debug" schemapb:"default=false"`
    Mode    string        `json:"mode" schemapb:"default=safe;in=[\"fast\",\"safe\"]"`
    Label   string        `json:"label" schemapb:"default=;max_len=64"`
    Blob    []byte        `json:"blob" schemapb:"default=AQI=;min_len=1"`
    Since   time.Time     `json:"since" schemapb:"default=2026-09-27T10:20:30Z"`
    Contact string        `json:"contact,omitempty" validate:"email"`
    Code    string        `json:"code,omitempty" pattern:"^[a-z]+$" desc:"Short code"`
}
```

Import `time` for this example. `secret=true` marks schema metadata; values remain
available to the application. It is not encryption or automatic log redaction.

Defaults only fill absent keys. Explicit null is checked against nullability;
explicit zero, false and empty strings are preserved.

| Kind | Typical attributes |
| --- | --- |
| String | `default`, `min_len`, `max_len`, `pattern`, `in`, `format` |
| Integer / float | `default`, `gt`, `gte`, `lt`, `lte` |
| Boolean | `default` |
| Duration | `default=30s`, `gt=0s`, other duration bounds |
| Timestamp | RFC 3339 `default`, timestamp bounds |
| Bytes | Base64 `default`, `min_len`, `max_len` |
| List | `min_items`, `max_items` |
| Any field | `required`, `nullable`, `secret`, `description`, `rules` |

Attribute names come from schemapb's protobuf descriptors. Both snake_case and
lowerCamelCase names work, for example `min_len` and `minLen`. Unknown attributes,
duplicates, invalid values and changes to field names/kinds fail reflection.

## Nested objects and collections

```go
type Endpoint struct {
    Host string `json:"host" schemapb:"default=localhost;min_len=1"`
    Port int64  `json:"port" schemapb:"default=8080;gte=1;lte=65535"`
}

type Service struct {
    Primary Endpoint            `json:"primary" schemapb:"default={}"`
    Backup  *Endpoint           `json:"backup"`
    Peers   []Endpoint          `json:"peers,omitempty" schemapb:"min_items=1;max_items=8"`
    Tenants map[string]Endpoint `json:"tenants,omitempty"`
}
```

- Missing `primary` becomes `{}`, then receives child defaults.
- Missing `backup` stays nil; a supplied `{}` receives child defaults.
- Each supplied peer or tenant object gets its own child defaults and validation.
- Missing `peers` is allowed; explicit `[]` fails `min_items=1`.
- Map keys must be strings. Fixed Go arrays imply fixed list length.

The pinned schemapb version supports only `{}` for Object/Ref defaults. Nonempty
object defaults and list/map defaults are rejected. Put nonempty base collections
in a lower-priority source instead.

## Values and escaping

Separate assignments with semicolons. Strings can be bare or JSON quoted:

```go
type Labels struct {
    Empty string `json:"empty" schemapb:"default="`
    Text  string `json:"text" schemapb:"default=\"a;b\""`
    Mode  string `json:"mode" validate:"oneof=fast safe" schemapb:"default={\"stringValue\":\"safe\"}"`
    Name  string `json:"name" schemapb:"rules=[{\"expr\":\"this != ''\",\"message\":\"must not be empty\"}]"`
}
```

Quote strings containing semicolons or unbalanced brackets. Repeated attributes,
map attributes and message attributes use ProtoJSON syntax. For `oneof` choices,
the default is a typed schemapb value, as shown above. A string with `in=[...]`
remains a string and accepts an ordinary string default.

## Supported `validate` tags

Reflection recognizes this subset of the go-playground vocabulary:

| Tag | Supported use |
| --- | --- |
| `required` | Require field presence |
| `gt`, `gte`, `lt`, `lte` | Numeric comparisons |
| `min`, `max` | Numeric bounds, string/byte length, slice size |
| `len` | String/byte length |
| `oneof=fast safe` | String choices; signed integer choices are also supported |
| `email`, `url`, `uuid`, `ip`, `ipv4`, `ipv6`, `hostname` | String formats |

This does not run go-playground/validator. Unrecognized `validate` tokens are
ignored by the pinned reflection implementation. In particular, do not rely on
`dive`, `required_if`, `eqfield`, or `validate:"omitempty"`. Nested Go struct fields
are reflected recursively without `dive`; use CEL `rules` for custom conditions.
Map size constraints are not inferred from `validate:"min=...,max=..."`.

Tag precedence is: type inference / `WithType`, then `json` / `validate` / `desc` /
`pattern`, then `schemapb`, then `WithFieldTags` callbacks. A schemapb attribute
replaces the corresponding inferred attribute.

See [schemapb reflection documentation](https://github.com/gopherex/schemapb/blob/master/go/README.md#reflection-attributes)
for custom tag callbacks and the schema builder API.
