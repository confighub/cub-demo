# Data model

What `cub demo up` creates in a ConfigHub organization, and the conventions that make it
queryable. The well-known Space labels are the product's (`Variant`, `Stage`, `Environment`,
`Region`, `Layer`, `Owner`); the tool adds `DemoName`, `Cluster`, `Department` and `Role`.

Every entity carries `DemoName=<scenario name>`. It is the cleanup key and the guard on every
org-wide query the tool issues.

## Components

One Component entity per catalog component and workload, slug = the component's name, labels
`Layer` (`platform` or `workload`), `Owner` and `Department`. A Component is in no Space; the
Spaces of its tree name it with `ComponentID`.

## Spaces

| Kind | Slug | Labels | Contents |
|---|---|---|---|
| Home | `<name>-platform` | `Layer=demo` | the ChangeWorkflows, the org-wide Filters and Views |
| Scenario | `<name>-scenario` | `Layer=demo` | the installed scenario definition ("cub demo install"), one AppConfig/YAML unit per file (path in the `cub-demo.confighub.com/path` annotation) |
| Cluster | `<region>-<class><n>` for shared clusters, `<region>-<dept>-<class><n>` for departmental ones — `us-east-prod2`, `eu-central-payments-prod1` | `confighub.com/cluster=true`, `Cluster=<slug>`, `Region`, `Environment` and `Stage` = class, `Department`, `Layer=cluster` | one Target named `cluster`, with facts |
| Component root base | `<c>-base` | `ComponentID` = the Component; `Variant=base`, `Role=base`, `Layer=platform` or `workload`, `Owner`, `Department` | one `Kubernetes/YAML` unit per manifest file; no target |
| Class base | `<c>-<class>` — `cert-manager-prod` | as root plus `Variant=<class>`, `Stage` and `Environment` = class, `Role=base`; annotation `UpstreamSpaceID` = root base | clone of the root; class policy applied by functions |
| Deployment | `<c>-<cluster>` — `cert-manager-us-east-prod2` | as class base plus `Variant=<cluster>`, `Cluster`, `Region`, `Department`, `Role=deployment`; annotation `UpstreamSpaceID` = class base; `ReleaseTargetID` = the cluster's target | clone of the class base; region and cluster values applied by functions; one published Release, carrying its live status |

Each component is a tree: root base → one base per class → one deployment per cluster the
component is placed on. `Role` is what separates bases from deployments in selectors.

## Targets

A Target names no worker, so each cluster's Target stands alone: it is where the cluster's
deployment Spaces publish their Releases. Target facts, queryable with
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

Root-base units carry `DemoName`, `Component` (the component's name, a label of the tool's
own: a unit has no `ComponentID`) and `Layer`. Clones inherit those; deployment units
additionally get `Cluster`, `Region`, `Stage`, `Department` so that org-wide unit queries and
Views can group by them. Unit slugs are the manifest file names (`namespace`, `controller`,
`cluster-issuer`, `db`, …).

Crossplane managed resources (`rds.aws.upbound.io` `Instance`, `s3.aws.upbound.io` `Bucket`,
`sqs.aws.upbound.io` `Queue`, `elasticache.aws.upbound.io` `ReplicationGroup`) are ordinary
`Kubernetes/YAML` units inside the workload that uses them, with `spec.forProvider.region` set
per deployment.

## Live status

The latest Release of a deployment Space carries `LiveStatus`, as argobot would report it for
the Argo CD Application syncing that Space:

```json
{"Reporter":"cub-demo/argocd","DataSource":"cert-manager-us-east-prod2","Sync":"Synced","Health":"Healthy","Operation":"Succeeded","ReporterSync":"Synced","ReporterHealth":"Healthy","ReporterOperation":"Succeeded","ObservedAt":"…"}
```

The story scatters a small, deterministic set of `Degraded`, `OutOfSync` and `Progressing`
exceptions, and leaves a small set of deployments with unreleased changes. A deployment that
was never released has no Release and therefore no status.

## Change workflows

Shared `ChangeWorkflow` entities in the home Space, one per distinct class coverage —
`standard-rollout` for components placed on every class, `rollout-<classes>` otherwise. A
workflow names no component: ChangeOrders are created in a component's root base with
`--change-workflow`, binding one at creation, and the change order's Space supplies the
Component every stage selector is narrowed by. The story's change orders are promoted stage by
stage and each stage's Releases are published for the change order, so one walked through its
last stage and reported healthy reads `Completed`. Stage shape in `DESIGN.md`.

## Selectors worth knowing

```
cub space list  --where "Labels.DemoName = 'meridian' AND Labels.Layer = 'cluster'"
cub space list  --component cert-manager --where "Labels.Role = 'deployment' AND Labels.Stage = 'prod'"
cub unit list   --where "Labels.DemoName = 'meridian' AND Labels.Region = 'eu-central'"
cub target list --where "Facts.Cluster.Department = 'payments'"   # facts keys are written unquoted
cub component list --where "Labels.Layer = 'workload'"
```
