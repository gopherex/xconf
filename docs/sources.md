# Source integrations

All sources return complete raw layers. Order in `Load` / `Open` determines priority;
later sources override earlier ones. Defaults, coercion and validation run once after
merge. An unsuccessful reload retains the last valid snapshot and emits an error.
No adapter writes to its backing store or manages application credentials.

## Allowed paths

`xconf.AllowPaths` wraps any source. For a Consul layer that may override only live
settings:

```go
import consulconf "github.com/gopherex/xconf/contrib/sources/consul"

remote := consulconf.NewPrefix(client.KV(), "apps/api",
    consulconf.IgnoreKeys("_*"),
)
live := xconf.AllowPaths(remote,
    xconf.Path{"server", "limit"},
    xconf.Path{"logging"},
)
runtime, err := xconf.Open(ctx, schema, base, live)
```

`server.limit` and the whole `logging` subtree can come from Consul. Other fields
keep values from `base` or schema defaults. The same wrapper works with file,
HTTP, environment, memory and custom sources; the caller supplies the allowlist.

- Each `Path` contains literal object/map keys: `Path{"labels", "a/b"}` selects
  the map key `a/b`. Dots, slashes and `*` are not path separators or wildcards.
- No paths allows nothing; an explicit empty `Path{}` allows everything. Duplicate
  and overlapping paths are harmless. The wrapper captures the paths at construction.
- A selected path includes all descendants, including null, zero, false and empty
  containers. Missing paths contribute nothing. An unselected scalar/null ancestor
  contributes nothing even if a child is allowed.
- Lists must be selected whole. OneOf fields may be selected individually except
  the discriminator: changing it requires allowing the entire OneOf object.
- `Replace`/`Delete` edits inside selected subtrees retain their order. Edits on an
  ancestor are restricted to selected subtrees. An ancestor `Replace` deletes
  selected children absent from its replacement; excluded siblings remain intact.
- Filtering runs after `Source.Read`, before merging and schema validation. It
  does not suppress I/O, decoding or source path-conflict errors. `IgnoreKeys` can
  still exclude Consul metadata before the adapter parses and assembles its layer.

The wrapper forwards watch registration, cleanup, source name, errors and revision.
Locations for retained paths and their ancestors remain available to provenance.
A successful read with no matching paths is an empty layer, not `ErrNotFound`.
Use `Optional` to handle a missing underlying source. Removing a live override
reveals lower layers/defaults on the next successful reload. Revision changes may
advance the snapshot version even when only excluded fields changed; consumers
should use `Event.Changed` to determine which values changed.

## Documents and embedded defaults

```go
import (
    json "github.com/gopherex/xconf/contrib/decoders/json"
    yaml "github.com/gopherex/xconf/contrib/decoders/yaml"
    files "github.com/gopherex/xconf/contrib/sources/fs"
    httpconf "github.com/gopherex/xconf/contrib/sources/http"
)

// defaults implements fs.FS, for example via //go:embed.
base := files.New(defaults, "config.yaml", yaml.Decode)
remote := httpconf.New(httpClient, "https://config.example/app.json", json.Decode)
runtime, err := xconf.Open(ctx, schema, base, remote)
```

`fs.New` has no watcher. It reopens the file on each read; wrap it in `xconf.Poll`
for a mutable filesystem. Missing paths return `ErrNotFound`. File names must be
valid `io/fs` paths. The caller retains ownership of the filesystem.

HTTP polls every 30 seconds; `Interval(0)` disables polling. Requests have a
10-second deadline configurable with `Timeout`. `Headers` copies caller headers.
ETag / Last-Modified validators are sent only after a successfully decoded response.
On 304 the cached raw document is decoded again into independent data; schema
defaults and merged values are never cached. A 404 clears the cache and returns
`ErrNotFound`. All other non-200 responses, including 204, fail the read. The source
name excludes URL userinfo and query parameters; use `Name` for a custom safe name.
Configure TLS, redirects and authentication on the caller-owned HTTP client.

Document transports default to an 8 MiB payload limit, configurable with `MaxBytes`.
The root module's `internal/document` package implements bounded reads and location
fallback for source adapters; it is not a public API or a separate module. HTTP and S3
bound streamed reads; NATS, Vault, Consul, etcd and Kubernetes enforce the limit after their SDK materializes
the response. Configure server/client limits too when untrusted payload size matters.

## Secret directories

```go
import secrets "github.com/gopherex/xconf/contrib/sources/directory"

source := secrets.New("/run/secrets", map[string]xconf.Path{
    "db_password": {"db", "password"},
    "api_token":   {"api", "token"},
})
```

Bindings select relative file names and exact schema paths. Nil bindings select
visible files as literal top-level keys; an empty map selects none. Contents remain
strings, including empty strings and whitespace. Enable schema coercion for numeric
or boolean fields. Objects and lists should use a document source instead.
`TrimFinalNewline()` explicitly strips one LF or CRLF; it never trims other spaces.
Overlapping paths and nonregular files are errors. At most 1024 selected files and
8 MiB total contents are accepted (`MaxBytes` changes the byte limit).

A missing directory returns `ErrNotFound`. A missing individual file contributes
no value, revealing lower-priority layers or schema defaults; required fields still
fail validation if no value is available. Unreadable files fail the entire source.
Polling defaults to 250 ms; `Interval(0)` disables it. A Kubernetes `..data` symlink
pins a generation while reading. A changed generation retries the whole read up to
four times. Ordinary directories have no multi-file transaction; use an atomic
document or projected generation when related keys must change together.

## S3-compatible storage

```go
import s3conf "github.com/gopherex/xconf/contrib/sources/s3"

source := s3conf.New(s3Client, "app-config", "production.yaml", yaml.Decode)
// Optional immutable selection: s3conf.Version("version-id").
```

Pass an AWS SDK v2 `*s3.Client`, configured by the application. AWS, MinIO and other
compatible endpoints use the same adapter; configure endpoint, credentials, region
and path-style access on the SDK client. Each read fetches a complete object.
Revision metadata uses VersionId, then ETag, then a content hash. Only `NoSuchKey`
and `NoSuchVersion` map to `ErrNotFound`; a missing bucket or denied access is an
error. Polling defaults to 30 seconds, request timeout to 10 seconds; `Interval`,
`Timeout`, `MaxBytes` and `Name` configure the source. No client is closed.

## Vault KV v2

```go
import vaultconf "github.com/gopherex/xconf/contrib/sources/vault"

source := vaultconf.New(vaultClient.KVv2("secret"), "apps/api",
    vaultconf.Name("vault:secret/apps/api"))
// Optional immutable selection: vaultconf.Version(3).
```

The KV data object is the raw configuration layer. The source copies it while
preserving JSON number tokens and records the Vault version as its revision.
Missing secrets, deleted versions and destroyed versions map to `ErrNotFound`.
Permission, network and malformed-response errors fail normally. Defaults are
30-second polling and a 10-second timeout. `Interval(0)` disables polling.
Use distinct `Name` values when reading the same path from different mounts.

Authentication, token renewal and namespace configuration belong to the injected
Vault client. This adapter supports static KV v2 data, not dynamic leased credentials.

## NATS JetStream KV

```go
import natsconf "github.com/gopherex/xconf/contrib/sources/nats"

bucket, err := js.KeyValue(ctx, "CONFIG") // js is jetstream.JetStream
if err != nil { return err }
source := natsconf.New(bucket, "production.api", json.Decode)
```

One key holds one complete document. Keys are exact, without wildcards. Revision
is the KV entry's sequence. Missing/deleted/purged keys return `ErrNotFound`.
The bucket must already exist, including when the source is wrapped in `Optional`.

Watch registration completes before the initial read. Put/delete/purge events
invalidate the layer. Watches carry metadata only. Closed watch streams are
recreated with bounded retry backoff; reconnection transport behavior belongs to
the NATS client. Periodic reconciliation every 30 seconds additionally detects
outages and missed notifications; `Interval(0)` leaves only native watch enabled.
Reads have a configurable 10-second timeout. Closing the runtime stops its
watchers and waits for callbacks, without closing the caller's NATS connection.
SDK watch registration uses the supplied context and the client's API timeout.

## Consul KV

```go
import consulconf "github.com/gopherex/xconf/contrib/sources/consul"

source := consulconf.New(consulClient.KV(), "apps/api/config", yaml.Decode)
runtime, err := xconf.Open(ctx, schema, defaults, xconf.Optional(source))
```

Pass `client.KV()` from a configured `github.com/hashicorp/consul/api` client.
Credentials, TLS, datacenter, namespace and partition belong to the caller.
Use `Name` to distinguish the same key in different clusters or datacenters.
`New` reads one exact key containing a complete document. `NewPrefix` maps a
subtree of keys into configuration fields (see below).
Reads request consistent data. Revision metadata records the key's `ModifyIndex`.
Missing keys return `ErrNotFound`; invalid documents, ACL denial and outages fail
the read. Optional key removal reveals lower-priority layers.

Watch captures an index synchronously before the runtime's initial read, then uses
[Consul blocking queries](https://developer.hashicorp.com/consul/api-docs/features/blocking).
Creation, updates and deletion invalidate the layer. A regressing index restarts
the query; zero indexes are bounded to prevent spinning. Errors retry with backoff,
and recovery invalidates even if the index is unchanged. A token bucket permits
two immediate watch requests, then limits sustained churn to one request per second.

`WaitTime` defaults to five minutes (maximum ten minutes); zero disables watch.
`Timeout` defaults to ten seconds for reads. Blocking requests allow the server
wait, its jitter, and this additional network timeout. Configure the injected HTTP
client's timeout to permit that duration. Close cancels outstanding queries and
waits for notifications to finish, retaining ownership of the caller's client.
`MaxBytes` applies to the decoded KV payload after the SDK reads the response;
Consul also enforces its own [KV size limit](https://developer.hashicorp.com/consul/api-docs/kv).

## Consul prefixes

```go
source := consulconf.NewPrefix(consulClient.KV(), "apps/api/")
// apps/api/db/host -> db.host
// apps/api/ports   -> ports (a JSON list if the schema declares a list)

// Exclude service metadata before constructing the config layer.
source = consulconf.NewPrefix(consulClient.KV(), "apps/api/",
    consulconf.IgnoreKeys("_*", "config_revision", "db/_*"))
```

The trailing slash is normalized. Values use the env adapter's schema-aware
encoding: strings for scalars, JSON with exact numbers for containers. Enable
schema coercion for textual scalar types. Empty directory markers are ignored;
empty leaf values remain present. Parent/child conflicts, duplicate paths, empty
path segments and keys outside the selected prefix fail the read. A prefix with
no value keys is `ErrNotFound`. Removing a key reveals lower layers on reload.
At most 1024 entries and `MaxBytes` total value bytes are accepted.

`IgnoreKeys` is opt-in and applies only to `NewPrefix`. Patterns use Go `path.Match`
syntax against relative keys, without `apps/api/`: `*` matches within one path
segment. Matching a parent also excludes its descendants, so `_*` skips `_revision`
and `_meta/nested/key`; `db/_*` skips metadata under `db`. Multiple options accumulate.
Pattern slices are copied. Invalid patterns, or using `IgnoreKeys` with the exact-key
`New` constructor, make `Read` and `Watch` fail.

Ignored keys contribute no values, locations or layer revision. Their updates can
trigger a Consul watch/read, but do not advance a healthy runtime's snapshot version
on their own. If only ignored keys remain, the source returns `ErrNotFound`, allowing
`Optional` to reveal lower layers. Unmatched keys retain normal schema validation.
ACL checks, prefix boundaries, the 1024-entry limit and `MaxBytes` still apply to the
full SDK response, including ignored entries.

One consistent List request supplies the entire layer. The blocking-query cursor
uses the query index, while layer revision hashes the selected keys, their modify
indexes and contents. ACL-filtered results fail rather than silently dropping
inaccessible configuration fields. The existing Consul watch/recovery contract
applies to both exact keys and prefixes.

## etcd v3

```go
import etcdconf "github.com/gopherex/xconf/contrib/sources/etcd"
source := etcdconf.New(etcdClient, "/apps/api/config", json.Decode)
```

The application configures endpoints, authentication and TLS on the caller-owned
etcd v3 client. A key contains one document. Missing keys return `ErrNotFound`;
invalid data and unavailable reads retain the runtime's last valid snapshot.
Revision is the key's `ModRevision`, not the revision of unrelated keys.

Watch captures a linearizable read revision and subscribes from its successor,
waiting for server acknowledgement before Open's initial read. Closed, canceled
or compacted watches re-read current state, register again and invalidate the
runtime so skipped history does not leave stale configuration. The SDK handles
transport reconnection. Reconciliation every 30 seconds also detects failures
or missed notifications (`Interval(0)` disables it). `Timeout` defaults to ten
seconds for reads and watch registration. Close cancels only this source's
watches, not the shared client.

## pflag and Cobra

```go
import flagconf "github.com/gopherex/xconf/contrib/sources/pflag"
source, err := flagconf.New(cmd.Flags(), map[string]xconf.Path{
    "listen": {"server", "address"},
    "debug":  {"debug"},
})
```

Call after parsing, for example from Cobra RunE, where inherited persistent
flags are available through `cmd.Flags()`. Only bound flags marked Changed are
captured; parser defaults never override lower layers or schema defaults. Explicit
false, zero and empty collections remain present. Values retain native boolean,
number, duration, byte, slice and map types. Repeated flags and aliases follow
pflag's parsing rules. Custom value types can supply `DecodeValue`.

Construction copies the selected values. The source does not retain a mutable
FlagSet, parse arguments, alter flags or watch process-global state. Rebuild it
if the application reparses flags. Unknown bindings and overlapping schema paths
are errors, including bindings to currently unmodified flags.

## Reader sources

```go
import readerconf "github.com/gopherex/xconf/contrib/sources/reader"
source := readerconf.New("custom", func(ctx context.Context) (io.ReadCloser, error) {
    return openConfig(ctx) // fresh reader on each reload
}, yaml.Decode)
```

The factory must honor context and return a fresh owned reader. Its Close must
be safe during Read and unblock a pending Read; cancellation closes it. Each
reader is closed exactly once, including parse failures and oversized documents.
Read and close errors fail the layer. `MaxBytes` defaults to 8 MiB. There is no
native watcher; manual reload or `xconf.Poll` invokes the factory again.

`FromReader(ctx, name, reader, decoder, options...)` instead captures a borrowed
stream once within the same byte limit. It does not close the borrowed reader;
the caller must arrange cancellation if that initial read can block. Subsequent
loads decode independently from the immutable capture.

## Environment file references

```go
import envfile "github.com/gopherex/xconf/contrib/sources/envfile"
source := envfile.New(map[string]xconf.Path{
    "DB_PASSWORD_FILE": {"db", "password"},
})
```

Only explicitly bound names ending in `_FILE` are followed. An absent variable
contributes no value. An empty file path, simultaneous `NAME` and `NAME_FILE`,
or a configured missing/unreadable/nonregular file is an error. `Optional` does
not suppress a broken file reference. Contents preserve whitespace and empty
strings; `TrimFinalNewline()` explicitly removes one LF/CRLF. Container fields
use JSON syntax, as in env. `Environment` injects variables without modifying the
process environment. Locations record both the variable name and file path.

Polling every 250 ms reopens files, detecting atomic replacements and new symlink
targets. `Interval(0)` disables polling. At most 1024 configured bindings and
8 MiB total file content are accepted (`MaxBytes` changes the total). Changes to
several unrelated files are not one transaction; use a document or projected
secret directory if coordinated updates are required.

## Kubernetes API

```go
import kubeconf "github.com/gopherex/xconf/contrib/sources/kubernetes"
config := kubeconf.ConfigMap(coreClient.ConfigMaps("app"), "api-config",
    kubeconf.Document("config.yaml", yaml.Decode))
secrets := kubeconf.Secret(coreClient.Secrets("app"), "api-secrets",
    kubeconf.Bindings(map[string]xconf.Path{"password": {"db", "password"}}))
```

Clients are scoped to a namespace by the caller. Kubernetes authentication,
TLS and credentials stay in the injected client. Use distinct `Name` values for
the same object name in different namespaces/clusters.

Without options, keys become literal top-level fields: ConfigMap Data and Secret
Data become strings; ConfigMap BinaryData stays bytes. Secret base64 decoding is
performed by the SDK, with no second decoding. Enable schema coercion for textual
scalars. `Bindings` selects and maps keys to nested paths; missing selected keys
contribute nothing. `Document` reads one selected data key through a decoder and
cannot be combined with Bindings. Missing objects or selected documents return
`ErrNotFound`. Whole-object contents are bounded by `MaxBytes` after the SDK read.
Malformed data, RBAC denial and connection failures fail normally.

Watch requires **get, list and watch** permissions on the selected resource type.
It lists with an exact `metadata.name` field selector, obtains the collection
resourceVersion even when the object is missing, and registers the watch before
Open's initial read. Creates, changes and deletes invalidate the layer. Closed
streams and error events (including expired resourceVersion/410) trigger relist
and resubscription with backoff, followed by a full reload. Bookmarks do not
publish config changes. Reconciliation defaults to 30 seconds (`Interval(0)`
disables it); `Timeout` bounds reads and watch setup. Active watch requests use
a five-minute server timeout, so configure client timeouts accordingly.
Closing the runtime cancels its requests and stops streams, leaving the client
usable. Reads from separate ConfigMaps/Secrets are not a cross-object transaction.

## Error handling and observability

`xconf.Optional` suppresses only `ErrNotFound`. It never hides malformed documents,
authorization failures, size limits or network errors. Revision and location are
available through snapshot metadata and `Explain`. Source error strings hide
provider payloads; unwrapping an error or logging raw diagnostics can expose data.
Use source names and locations that are safe for your logs.

External stores are read independently: a runtime publication is atomic within
the process, but there is no cross-store transaction or coordinated revision.
