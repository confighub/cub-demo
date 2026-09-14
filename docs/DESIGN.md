# Design

`cub demo` seeds a ConfigHub organization with a realistic, fleet-scale dataset from a
declarative scenario. This document records why it exists, the product facts it relies on, and
how the seeding pipeline works. `DATA-MODEL.md` defines what it creates; `ROADMAP.md` tracks
what is built.

Keep this document current. When a product fact below stops being true, fix the fact and the
code together.

## Why

ConfigHub's recent work — components, the variant graph, change workflows — is best experienced
on the data shape most customers have: on the order of a hundred Kubernetes clusters, a platform
catalog deployed to most of them, business workloads deployed sparsely, and cloud resources next
to the workloads that use them. No example or test fixture produced that shape; the previous
team dataset was a bash script for seven targets that predates the component model.

The tool is generic (a scenario file drives it) and ships one embedded scenario, a fictional
global enterprise, so anyone in the org can clone this repo, `make plugin`, and
`cub demo install meridian && cub demo up` their way to the fleet-scale experience. It is also a scale test of the product itself — it
found a server memory issue at seeding scale and a promotion-tagging bug, both since addressed.

Nothing is provisioned. Clusters are ConfigHub Targets; deployments are published Releases;
"live" state is written as the annotation a GitOps operator would report.

## Decisions

- **A `cub` plugin, in Go, on the public SDK** (`github.com/confighub/sdk/core`). The other
  plugins (`cub-helm`, `eks-inference`, `cub-server`, `cub-che`) set the conventions this repo
  follows: `plugin.HandleHook` manifest, `CUB_SERVER`/`CUB_TOKEN` from the environment.
  Installation is release-based (`cub plugin install confighub/cub-demo`); `make plugin`
  installs a local build in place for development — GitHub-release install
  does not work against private repos.
- **Scenario-file driven.** Dimensions, catalog, workloads, placement and story elements come
  from YAML. The embedded default is the product; a user can `cub demo scenario export` it and
  edit their own.
- **Rendered manifests as unit data**, hand-authored, ~100–300 lines per component, rather than
  `HelmRelease` objects. Kubernetes functions, diffs and the resource views need real resources
  to bite on. Business-specific values live in the manifests.
- **Variation is applied server-side by functions**, not by uploading per-variant renders. The
  root base is rendered once; class, region and cluster differences are `set-replicas`,
  `set-env-var`, `set-string-path` invocations over a `where`. Clones copy data server-side, so
  this is both the fast path and the one that leaves real revision history behind.
- **Crossplane for external resources** (provider-aws managed resources as units inside the
  workload that uses them). Cloud-neutral, and the shape `examples/eks-manager` and
  `examples/configboard` already understand.
- **Entity quotas are not a design input.** Default org quotas (100 Spaces, 250 Targets and
  Workers, 1000 Units and Links) are far below the dataset; they are raised server-side
  (`confighub admin quota set`). The tool reports a quota error clearly and does not shrink the
  demo to fit.
- **Workflows use the bases-first shape.** The product intends a workflow stage to span
  several levels of the variant hierarchy (a class base together with the deployments cloned
  from it); today's `cub variant promote` cannot do that. Rather than stage pairs per class,
  the tool generates one **"bases" stage holding every class base** — safe as an unordered
  wave because class bases are siblings of the root — followed by one stage per class's
  deployments, gated released+healthy on the class before. Only one artifact stage, the
  deployment gating intact, and the shape may well survive the eventual multi-level
  specification. (Jesper's call, 2026-08-31.)

## Product facts the design rests on

Verified against the ConfigHub server codebase on 2026-08-29. File paths are in that repo.

**Delivery model.** The bridge/apply path is gone. A Target is an address; configuration
reaches a cluster by publishing a Release (an OCI bundle of a Space) that Argo CD or Flux pulls.
A fake cluster is therefore a Space plus an OCI Target (`ProviderType: OCI`, `ToolchainType:
Any`, `Parameters: "{}"`, `Facts` map) bound to a **server-hosted worker**
(`ProvidedInfo{IsServerWorker: true, SupportedConfigTypes: [{OCI, Any}]}`, `OrgRole: none`),
which reports `Ready` with no process behind it. One worker can back every target. Recipe:
`public/cmd/cub/cluster_api.go` (`clusterCreateOCIWorker`, `clusterCreateOCITarget`).

**Components and variants are label conventions**, not entities (`public/cmd/cub/component.go`,
`space_labels.go`). A component is the set of Spaces sharing a `Component` label; each Space is
one variant, named by `Variant`; a variant with no target is a base. Slug convention
`<component>-<variant>`. The upstream relation is the `UpstreamSpaceID` annotation on the Space
plus `UpgradeUnit` links and clone lineage on the units, all created by the server when a Space
and its units are bulk-cloned. The tree must be a tree.

**`cub variant create` is three API calls** (`public/cmd/cub/variant_create.go`):
`BulkCreateSpaces` selecting the upstream Space with `VariantLabels` and `NamePattern`
`template:{{.Labels.Component}}-{{.Labels.Variant}}` and a patch carrying `UpstreamSpaceID`;
`PatchSpace` setting `ReleaseTargetID`; `BulkCreateUnits` selecting the upstream Space's units
with `WhereSpace` naming the new Space. Two server behaviours let this fan out: `VariantLabels`
accepts `Key=v1|v2|v3` and produces the cross product (`internal/views/bulk_handlers.go`), and a
bulk unit clone whose patch carries no `TargetID` takes its **destination Space's
`ReleaseTargetID`** (`bulk_handlers.go`, `internal/views/unit.go`). So one `BulkCreateSpaces`
creates every deployment Space of a (component, class), and one `BulkCreateUnits` with a
`WhereSpace` over them clones the class base into all of them with the right targets.

**Unit data** is not part of the Unit JSON; it is uploaded with `UploadUnitData`
(`application/octet-stream`) and `Unit.DataHash` is its SHA-256. **Functions** run server-side
over an org-wide `where` via `InvokeFunctionsOnOrg` and record real Revisions and Mutations.
**Resource views** (`Of: Resource`, DataPath columns walking the stored config) are how
configuration content becomes table columns; both `cub resource list --view` and the View
Explorer evaluate them.

**A Release bundles a whole Space**, so bases and deployments must be separate Spaces (an earlier
example that kept both in one Space could not be ported to the release model).
`Space.ReleaseTargetID` must reference an existing Target and is `ON DELETE RESTRICT`, which fixes
creation and teardown order. `PublishRelease` builds the bundle server-side and advances
`Unit.LastReleasedRevisionNum`; a unit with `HeadRevisionNum > LastReleasedRevisionNum` shows as
having unreleased changes.

**Live status** is the Space annotation `confighub.com/live-status` holding JSON
`{source, app, syncStatus, healthStatus, operationPhase, revision, message, observedAt}`
(`public/core/livestatus/livestatus.go`, value ≤ 1024 bytes). The UI renders it on the component
graph; `cub variant promote`'s `healthy` prerequisite requires `Synced`, `Succeeded` and
`Healthy`, and `released` requires a Release published after the promotion.

**Change workflows today.** The definition is a KRM unit (`confighub.com/v1 ChangeWorkflow`,
toolchain `AppConfig/YAML`) with `spec.source.space` naming the tree root and
`spec.stages[]{name, whereSpace, prerequisites}`; a ChangeOrder is created in the source Space
with `--change-workflow`; `cub variant promote --change-order --target-stage` promotes a stage by
looping its Spaces client-side in unspecified order, and a base Space (no `ReleaseTargetID`)
can never satisfy `released`/`healthy`. Consequently a stage cannot currently contain a class
base and the deployments cloned from it. The intended product behaviour is that it should; the
specification is open (`docs/design/change-workflows.md` in the product repo).

**Plugin protocol.** Plugins live in `~/.confighub/plugins/<name>/` with a `cub-plugin.yaml`
that `plugin.HandleHook` writes at install time. `cub plugin install confighub/cub-demo` strips
the `cub-` prefix, so the command is `cub demo`. cub execs the plugin with `CUB_SERVER`,
`CUB_TOKEN`, `CUB_CONTEXT`, `CUB_CONFIG` in the environment; `cub` itself is on `PATH`.

## The tool/scenario boundary

The tool owns lifecycle (`up`, `down`, `reset`, `status`, `plan`, `list`, `scenario`) and
generic capabilities; the scenario owns every named move, all demo nouns, and the
choreography. A verb that mentions a bot, a pipeline, or a beat of a particular demo is on
the wrong side of the line.

Concretely: `plays:` in the scenario maps names to step sequences, run with `cub demo play
<name>`. A step is a shell line (`run:`) or a call to a tool primitive (`call:` + `with:`) —
`observe` (write a live-status observation by selector, optionally only where a release is
newer than the last observation), `invoke` (one ConfigHub function against one unit),
`bump-image` (read the base's current tag, bump it, write it back; exports `.Version` to the
steps after it), `changeorder` (create one under the component's workflow). Strings are
templates over the scenario plus the exported variables. So the workflows scenario's "ship"
and "argobot-test" are its own vocabulary; the tool never had to hear of either.

## Multiple demos

A demo is a scenario; orgs keep them apart. Three mechanisms make several demos safe and
self-describing:

- **Context pin.** A scenario may declare `context: <cub context name>`; up, down and status
  refuse when the active context differs, so a pinned demo cannot fire into the wrong org.
- **Provenance is the definition.** `install` stores the entire scenario bundle — scenario.yaml,
  every component.yaml and manifest, the external templates — as AppConfig/YAML units in a
  `<name>-scenario` space, one unit per file, hash-skipped like everything else. The org itself
  answers "what defines this demo, at which revision", scenario updates land as ordinary
  revisions, and every other verb reads the definition back from this space. Manifest templates
  must therefore be valid YAML: template expressions that begin a scalar or key are quoted.
- **`cub demo list`** groups the org's spaces by DemoName: which demos live here, how big.

Per-stage workflow gates are configurable per scenario (`workflows.prerequisites`); the
`workflows` scenario uses this to make the gates themselves the demo.

## The pipeline

`cub demo up` runs phases in order. Inside a phase, independent work runs on a bounded pool
(`--concurrency`, default 8). Every entity is labelled `DemoName=<scenario name>`, and every
org-wide `where` the tool issues includes that label, so nothing outside the demo is selected.

1. **home** — the home Space; the server-hosted worker.
2. **clusters** — per cluster: Space, then OCI Target with facts.
3. **bases** — per component: root base Space with one unit per manifest file (data uploaded,
   skipped when `DataHash` already matches); class bases in one `BulkCreateSpaces`; their units in
   one `BulkCreateUnits`; class policy as one `InvokeFunctionsOnOrg` per (component, class).
   Class policy is applied before phase 4 so deployments clone the class data.
4. **deployments** — per (component, class): deployment Spaces in chunked `BulkCreateSpaces`;
   per cluster: `BulkPatchSpaces` setting `ReleaseTargetID` and cluster labels; per (component,
   class): chunked `BulkCreateUnits` cloning the class base into the deployment Spaces (clones
   take their Space's target); region and cluster functions; unit labels per cluster.
5. **releases** — `PublishRelease` per deployment Space, skipping the story's deliberately
   unreleased set and any Space with nothing unreleased.
6. **status** — the healthy live-status annotation in bulk per (component, class), then the
   story's Degraded / OutOfSync / Progressing exceptions individually.
7. **views** — org-wide Filters and Views in the home Space.
8. **workflows and stories** — workflow units and in-flight ChangeOrders (roadmap milestone 6).

**Slugs are org-global and scenarios must not collide.** The seeder refuses to adopt a space
whose `DemoName` label names another owner (or none), so a colliding scenario fails loudly
instead of nesting demo entities in somebody else's space. The `e2e` scenario's regions and
component names are deliberately disjoint from meridian's for this reason. A component listed
in the scenario without authored manifest files is skipped by the seeder and reported by
status as pending content, so the scenario can carry the full vision ahead of the content.

**Idempotent and resumable.** Each phase starts from an index of the org's demo Spaces; creates
use `AllowExists` or get-by-slug; data uploads compare hashes; releases skip when nothing is
unreleased; function and patch phases record a marker annotation on the home Space
(`confighub.com/demo-phase-<n>=<scenario hash>`) and are skipped until `--force-phase`. Errors
are collected per item — bulk requests answer 207 with per-entity results — and reported with
slugs at the end; a quota error names the `confighub admin quota set` command that lifts it.

**Teardown** follows the foreign keys: deployment Spaces (recursively, taking units, links and
releases) → class bases → root bases → cluster Spaces (targets) → home Space (the worker last).
`down` and `reset` keep the `<name>-scenario` space, so the demo stays installed; only
`uninstall` deletes it.

**Never `--wait`.** Unit writes enqueue link resolution server-side; the tool does not block on
it. The only ordering that matters — release after all data writes to a Space — is enforced by
the phase boundaries.

## Commands

The verb split mirrors the product: **`install` writes the definition into the org as config
data** (the `<name>-scenario` space — one unit per bundle file), and **`up` reconciles the org
to it**. A scenario name or file appears only on `plan` and `install`; every other verb
discovers the installed definition by reading that space back, so operating a demo needs no
local files and the org is always acted on as it was actually defined. `--demo <name>`
disambiguates when one org holds several demos (plays export it to child commands as
`CUB_DEMO_DEMO`).

| Command | Talks to server | Purpose |
|---|---|---|
| `cub demo plan [scenario] [-o table\|json]` | named: no | expand a definition (or the installed one) and print clusters, the component × class matrix, placements and entity totals |
| `cub demo install <scenario> [--dry-run]` | yes | store a definition (embedded name or file path) in the org |
| `cub demo up [--phase …] [--concurrency N] [--dry-run]` | yes | converge the org to the installed definition; resumable |
| `cub demo status [--demo N]` | yes | what exists versus what the installed scenario describes, per phase |
| `cub demo down [--demo N] [--yes] [--force]` | yes | delete the dataset; the installed definition stays |
| `cub demo uninstall [--demo N] [--yes]` | yes | down, plus the `<name>-scenario` space: nothing remains |
| `cub demo reset [--yes]` | yes | down + up: back to the opening state |
| `cub demo play [<play>] [--dry-run]` | yes | run one of the scenario's plays |
| `cub demo list` | yes | the demos installed in the org, by `DemoName` |
| `cub demo scenario list \| show \| export` | no | inspect or copy the embedded scenarios |
| `cub demo version` | no | |

## Scenario file

The schema is defined in `internal/scenario/schema.go`; the authoring reference — scenario.yaml,
component.yaml, template contexts, plays and the rules that bite — is
[authoring-scenarios.md](authoring-scenarios.md), and `scenarios/` holds the embedded worked
examples. The design principle: manifests are rendered once for the root base, and all class,
region and cluster variation is applied by ConfigHub functions, so every difference is real
recorded history rather than templating.

A scenario given by path (`cub demo install dir/x.yaml`) resolves component directories from
`dir/manifests/<dir>` first and falls back to the embedded ones, so `cub demo scenario export`
followed by editing a few files is the customization path.
