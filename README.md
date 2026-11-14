# quasar — c12s configuration schema service

Stores versioned **YAML→JSON schemas** per `(org, namespace, name, version)`
in etcd and validates configurations against them. It is the validation step
in the config write path and the schema-management backend behind the
`cockpit schema` commands.

> The Go module is still `github.com/jtomic1/config-schema-service` (the
> service predates its c12s naming, like gravity/`agent_queue`); the
> repository, image and container are `quasar`.

## Role in c12s

```
cockpit create schema / validate ──▶ gateway ──▶ quasar ──▶ etcd (quasar_etcd, schema YAML)
                                                    ├──▶ meridian (namespace must exist; user/org context)
                                                    └──▶ oort     (async: org→schema inheritance)

kuiper Put/Place (config with schema) ──▶ quasar.ValidateConfiguration
cockpit image apply (fleet spec)      ──▶ quasar.ValidateConfiguration
                    ◀── {isValid, message} ──┘
```

Schemas are stored as YAML that must be convertible to valid JSON Schema;
validation uses [gojsonschema] against the stored schema. The namespace
referenced by `schema_details` must already exist in Meridian.

## API (`proto/config_schema.proto`, reflection enabled)

| RPC | Behavior |
|---|---|
| `SaveConfigSchema` | checks namespace exists (Meridian), authorizes, validates the schema document, requires the version to be **strictly newer** (semver) than the latest stored one, rejects an existing key, stores it, then asynchronously adds the org→schema Oort relationship |
| `GetConfigSchema` | fetches one schema by full details |
| `ValidateConfiguration` authz before lookup | The permission check runs on the schema id first, so validating against a schema that was never saved answers `permission denied: config.get`, not "not found" (cockpit's `image apply` explains this case). |
| `DeleteConfigSchema` | deletes one schema key |
| `ValidateConfiguration` | fetches the schema and validates the given YAML configuration against it (returns `isValid` + message) |
| `GetConfigSchemaVersions` | lists stored versions by `org/namespace/name` prefix |

### Fleet specs (OS management)

`cockpit image apply` sends every fleet spec, as written, to
`ValidateConfiguration` against the schema `<org>/default/host-image-spec`
(version `v1.0.0` by default) before planning. The schema lives in
`tools/bootc/specs/host-image-spec.schema.yaml` and is registered once per
org with `cockpit create schema` (after the first claim, which creates the
`default` namespace). The reply message lists **every** violation, joined
with `; ` (`path: problem`), so a client can show them all at once.

## Running

Compose service `quasar` (port `QUASAR_PORT`, 9090), reflection enabled, with
a unary interceptor that extracts the `authz-token` metadata into the context.

```bash
go build ./...   # module name differs from the repo directory name
go run ./cmd/server
```

## Configuration (env)

| Variable | Used for |
|---|---|
| `SERVER_PORT` | gRPC listen port |
| `ETCD_ADDRESS` | quasar's own etcd (`quasar_etcd:2379`, volume `quasar_etcd_data`) |
| `NATS_ADDRESS` | async Oort administration client |
| `SECRET_KEY` | JWT permission checks |
| `MERIDIAN_ADDRESS` | namespace lookups |

## Data and dependencies

- **etcd**: keys `<org>/<namespace>/<schemaName>/<version>` → schema YAML.
  A version is immutable once written (an existing key is rejected).
- **Meridian**: namespace existence is a precondition for saving; schema
  deletion does not touch Meridian.
- **Oort**: one async `org → schema` inheritance relationship per saved
  schema (fire-and-forget).

## Status

Stable for schema CRUD and validation as used by Kuiper and cockpit; the
error-model and permission-mapping warts below are accepted for the lab.

## Bugs, disabled and unfinished features

### Confirmed bugs / design warts

| Where | Effect |
|---|---|
| `internal/services/authorization.go` | Schema operations are authorized with **`config.put` / `config.get`** (`PermSchemaPut/Get/Del` map to config permissions) — there is no distinct schema permission. Anyone allowed to write configs in a namespace can also create/delete its schemas. |
| `internal/configschema/configschema.go` — `ValidateConfiguration` | When the schema lookup fails, the response message is **"Error while instantiating database client!"** even though the client was created fine (missing schema or etcd failure) — misleading diagnosis. |
| Mixed error model | Permission failures return a plain `fmt.Errorf` (surfaces as gRPC `Unknown`), validation failures return `status: 3` **inside a successful response**, etcd failures `status: 13` in the response. Clients (kuiper, cockpit) must handle both shapes. |
| `repository.NewClient()` per RPC | A new etcd client (with `defer Close`) is created on **every request** instead of one shared client — extra connections/latency, and client construction errors cannot be distinguished from store errors in the messages above. |
| `DeleteConfigSchema` | Removes the etcd key only; the `org → schema` Oort relationship stays behind (same fire-and-forget pattern as kuiper). |

### Disabled / unfinished features

- **Few tests**: `internal/configschema/validate_test.go` covers validation
  and the all-errors message; version ordering and the etcd repository have
  none.
- `validators.IsConfigurationValid` is a placeholder (checks non-empty
  only); the real validation happens in `configschema.validateConfiguration`
  — the dead-looking helper is a trap for readers.
- No schema *update* path: a fix means saving a new higher version (by
  design, but there is no deprecation/cleanup story for old versions).

### PoC boundaries (by design, not bugs)

- Versions must be SemVer **with a `v` prefix** (`v1.0.0`) — enforced by the
  validators; other strings are rejected.
- Plaintext gRPC on the compose network; `SECRET_KEY` is the shared lab
  signing key.
- JSON Schema support follows `gojsonschema`: drafts 04, 06 and 07 (`const`,
  `propertyNames` work), not 2019-09 / 2020-12.

References: `tools/docs/README.md` (corrected API conventions — config
validation YAML root key = config name, `schema_details` requires
`namespace`), `../kuiper/README.md` (main caller), `../plan/architecture.md`.

[gojsonschema]: https://github.com/xeipuuv/gojsonschema
