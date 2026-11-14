# tools

**Shared Kubernetes building blocks for Inrun, KOME and other intentd tools.**

The general-purpose code under [Inrun](https://inrun.dev) and [KOME](https://github.com/intentd/kome), written once. Nothing in it knows about either: anything product-specific (labels, annotation prefixes, field managers, names) is a parameter with a neutral default.

## Packages

| Package | Holds |
| --- | --- |
| `kube` | Kubernetes knowledge that needs no client of its own: CRD files and schemas (`CompareSchemas`, `SchemaLeafPaths`, schema path helpers), GVK/GVR, names, field selectors, a Lease-guarded `RunExclusively`, labels, annotations and finalizers, sentinels (what changed between two versions of an object), Secrets and their rotation, events |
| `kubeclient` | The client `Interface` and its implementation, and the object types |
| `kubeclient/kubefake` | The fake client every test uses |
| `builtins` | The table of built-in kinds and their lookups |
| `children` | Reading an object's live children and enriching them (pods, endpoints, PVC → PV, events, …) |
| `condition` | Conditions on a data map: comparisons, `when` (all) and `or` (one), time, day and cron windows |
| `resolver` | Go templates over a data map with a caller's function map, user functions, and `ApplyField` (writing a value through a template) |
| `schemagen` | Editor JSON Schemas from Go types |
| `cluster` | kind, kubectl and helm for CLIs: clusters, Helm releases, waiting for resources, Deployment health |
| `cluster/access` | Giving a hub cluster least-privilege access to target clusters: short or long tokens, refresh, revoke, and the credential as a Secret, Argo CD's cluster Secret or a kubeconfig |
| `structured`, `fileio`, `duration`, `retry`, `logger`, `term`, `version` | Nested paths and maps, file reading, durations with days and weeks, retry with backoff, logging, CLI output, build version |

## Rules

1. **No product code.** Nothing here imports Inrun or KOME, or names their labels or config.
2. **Small dependencies:** client-go, apimachinery, apiextensions, controller-runtime, YAML, a cron parser, zerolog, and invopop/jsonschema. Not KEL: the resolver takes its function map from the caller.
3. **Each package stands alone** and is named for what it holds.
4. **Pure packages don't log.** They return values and errors.
5. **Tests fake the cluster with `kubeclient/kubefake`.**

## Version

Binaries built on tools set their version at build time:

```console
go build -ldflags "-X github.com/intentd/tools/version.Version=v1.0.0 \
  -X github.com/intentd/tools/version.Commit=$(git rev-parse --short HEAD)"
```
