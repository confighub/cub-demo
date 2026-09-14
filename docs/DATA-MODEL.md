# Data model

What `cub demo up` creates in a ConfigHub organization, and the conventions that make it
queryable. The well-known Space labels are the product's (`Component`, `Variant`, `Stage`,
`Environment`, `Region`, `Layer`, `Owner`); the tool adds `DemoName`, `Cluster`, `Department`
and `Role`.

Every entity carries `DemoName=<scenario name>`. It is the cleanup key and the guard on every
org-wide query the tool issues.

## Spaces

| Kind | Slug | Labels | Contents |
|---|---|---|---|
| Home | `<name>-platform` | `Layer=demo` | the server-hosted worker, the ChangeWorkflow units, the org-wide Filters and Views |
| Scenario | `<name>-scenario` | `Layer=demo` | the installed scenario definition ("cub demo install"), one AppConfig/YAML unit per file (path in the `cub-demo.confighub.com/path` annotation) |
| Cluster | `<region>-<class><n>` for shared clusters, `<region>-<dept>-<class><n>` for departmental ones — `us-east-prod2`, `eu-central-payments-prod1` | `confighub.com/cluster=true`, `Cluster=<slug>`, `Region`, `Environment` and `Stage` = class, `Department`, `Layer=cluster` | one OCI Target named `cluster`, with facts |
| Component root base | `<c>-base` | `Component`, `Variant=base`, `Role=base`, `Layer=platform` or `workload`, `Owner`, `Department` | one `Kubernetes/YAML` unit per manifest file; no target |
| Class base | `<c>-<class>` — `cert-manager-prod` | as root plus `Variant=<class>`, `Stage` and `Environment` = class, `Role=base`; annotation `UpstreamSpaceID` = root base | clone of the root; class policy applied by functions |
| Deployment | `<c>-<cluster>` — `cert-manager-us-east-prod2` | as class base plus `Variant=<cluster>`, `Cluster`, `Region`, `Department`, `Role=deployment`; annotation `UpstreamSpaceID` = class base; `ReleaseTargetID` = the cluster's target | clone of the class base; region and cluster values applied by functions; one published Release; the `confighub.com/live-status` annotation |

Each component is a tree: root base → one base per class → one deployment per cluster the
component is placed on. `Role` is what separates bases from deployments in selectors.

## Targets and workers

One worker, `server-worker`, in the home Space, server-hosted (`IsServerWorker`), supporting
`{OCI, Any}`. Every cluster's Target binds to it. Target facts, queryable with
`cub target list --where "Facts.Cluster.KubernetesVersion = '1.32'"`:

| Fact | Example |
|---|---|
| `Cluster.Name` | `us-east-prod2` |
| `Cluster.KubernetesVersion` | `1.31` |
| `Cluster.Class` | `prod` |
| `Cluster.Department` | `payments` |
| `Cluster.NodeCount` | `24` |
| `Cloud.Provider` | `aws` |
| `Cloud.Region` | `us-east-1` |

## Units

Root-base units carry `DemoName`, `Component`, `Layer`. Clones inherit those; deployment units
additionally get `Cluster`, `Region`, `Stage`, `Department` so that org-wide unit queries and
Views can group by them. Unit slugs are the manifest file names (`namespace`, `controller`,
`cluster-issuer`, `db`, …).

Crossplane managed resources (`rds.aws.upbound.io` `Instance`, `s3.aws.upbound.io` `Bucket`,
`sqs.aws.upbound.io` `Queue`, `elasticache.aws.upbound.io` `ReplicationGroup`) are ordinary
`Kubernetes/YAML` units inside the workload that uses them, with `spec.forProvider.region` set
per deployment.

## Live status

Deployment Spaces carry `confighub.com/live-status`:

```json
{"source":"cub-demo","syncStatus":"Synced","healthStatus":"Healthy","operationPhase":"Succeeded","observedAt":"…"}
```

The story scatters a small, deterministic set of `Degraded`, `OutOfSync` and `Progressing`
exceptions, and leaves a small set of deployments with unreleased changes.

## Change workflows (milestone 6)

One `ChangeWorkflow` unit per component, in the home Space, rendered from the scenario's
workflow template; ChangeOrders are created in the component's root base. The stage shape is
deliberately not fixed here — see the decision in `DESIGN.md`.

## Selectors worth knowing

```
cub space list  --where "Labels.DemoName = 'meridian' AND Labels.Layer = 'cluster'"
cub space list  --where "Labels.Component = 'cert-manager' AND Labels.Role = 'deployment' AND Labels.Stage = 'prod'"
cub unit list   --space '*' --where "Labels.DemoName = 'meridian' AND Labels.Region = 'eu-central'"
cub target list --space '*' --where "Facts.Cluster.Department = 'payments'"   # facts keys are written unquoted
cub component list --where "Labels.Layer = 'workload'"
```
