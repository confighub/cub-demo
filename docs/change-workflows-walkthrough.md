# A version bump, from CI to prod

*How one change moves through a fleet in ConfigHub, and what each person along the way gets to
see before they say yes.*

This walkthrough assumes you know the vocabulary — components, variants, change orders, change
workflows and their stages and gates — from the change workflows guide. Here we watch a single
change go through the motions in the Meridian demo environment: one region, six clusters (one
dev, two test, three prod), and a workload called **catalog-api** that runs on all of them. Its
change workflow has four stages, `bases → dev → test → prod`, then a final check. Test may only
take a change that dev has *released*; prod may only take a change that test has released *and*
that is *healthy* there. That is the whole policy, and it is one ChangeWorkflow:
`cub changeworkflow get catalog-api-workflow --space workflows-platform` shows the four stages,
each a selector over spaces with the component left out on purpose — the change order supplies
it, so one workflow shape serves every component that rolls out the same way.

> **Presenter.** Setup, reset and the CLI form of every beat are in the appendix,
> [change-workflows-demo.md](change-workflows-demo.md). Spots marked `▶` are where you run
> something outside the UI — always one of the scenario's *plays*, `cub demo play <name>`, which
> prints the cub commands it stands for. Spots marked `⚠` depend on a product change that is not
> in yet; see the end. Verified 2026-09-12 on hub.confighub.com v0.4.15 with cub v0.4.16.

## Chapter 1 — a simple version bump

### 1. CI ships 5.3.0

The retail platform team merges the pricing engine rewrite. Their pipeline builds
`catalog-api:5.3.0`, and its last step tells ConfigHub — the same way the ConfigHub team's own
release pipeline does for the product. Two edits to the base, then a change order that names them.
The presenter runs one play, and the screen shows the three commands it stands for:

```
▶ cub demo play ship

▶ bump-image
catalog-api runs 5.2.0; shipping 5.3.0
▶ cub function set --space catalog-api-base --unit api --change-desc "catalog-api 5.3.0" \
      -- set-image-reference api :5.3.0
▶ invoke
▶ cub function set --space catalog-api-base --unit api --change-desc "catalog-api 5.3.0: memory limit 1Gi" \
      -- set-string-path apps/v1/Deployment spec.template.spec.containers.0.resources.limits.memory 1Gi
▶ changeorder
▶ cub changeorder create --space catalog-api-base catalog-api-5-3-0 --description "catalog-api 5.3.0" \
      --change-workflow workflows-platform/catalog-api-workflow
```

The first two commands are ordinary edits: two revisions on the base's `api` unit, each with the
pipeline's words on it. The new image, and a memory limit raised from 512Mi to 1Gi because the
new engine wants headroom — a sensible default for the fleet.

The third command is the one that matters. A **change order** is created *after* the edits,
under the workflow, and it works out its own range: for every unit in the base, the start is the revision the downstream
variants last took from it, and the end is the unit's head. Both new revisions fall inside that
interval, so both travel together; the three units the pipeline did not touch are covered too,
with an empty range. The change order records which workflow governs it, and therefore where it
is headed: every catalog-api space in the org, in the order the workflow says.

Nothing has been deployed. Nothing downstream has changed. A change has been *declared*.

> **Presenter.** Because the range runs from "what the variants have taken" to head, *any* edit
> to the base since the last promotion would be swept into this change order. Make the release
> the only thing that touched the base — or bound it with `--end-tag` if something else did.

**Gaps.** *Demo:* done — the `ship` play is these three commands. *Product:* `changeorder create` has no start bound —
only `--end-tag` — so CI cannot fence its own release from other base edits; a ChangeSet is the
tool for that and the product's own release script describes one it no longer runs (F19).

### 2. It shows up

Open **Rollouts**. A new row: `catalog-api-5-3-0`, State *InProgress*, Age *seconds*, a stage
strip showing the change sitting at its source, and a Blocker column that says nothing is blocking
it. Every change order in the org that is still moving is in this list — this is where an
operator, a release manager or an auditor starts their day.

Click into it. You land in the component view for catalog-api in rollout mode: the variant tree
drawn as the workflow's stage lanes, with the base marked **The change starts here**.

**Gaps.** None blocking. The list hides "complete" rollouts by default, but its idea of complete
is "every stage promoted", which is wrong in a way step 8 shows (F21).

### 3. What is this change, exactly?

Before anyone promotes anything, a reviewer wants to see the change itself — not the pipeline's
description of it, the actual configuration diff.

Select the base. The side pane shows the change order's start-to-end diff on `catalog-api-base`:
two fields on one resource, the container image `5.2.0 → 5.3.0` and the memory limit
`512Mi → 1Gi`. The other units the change order covers — `namespace`, `config`, `catalog-db` —
are listed as untouched. That list matters:
it is the change order saying "I know about these, and I bring nothing for them," which is what
lets a later release pin all of them at once.

This is the review of the *change*. Every later review is of the change's *effect* on a
particular place.

**Gaps.** *Product, and a decision.* The UI does not render this today. Selecting the base shows
"The change starts here" and *No matrix: the base is where the change was made, so there is no
incoming change to compare*; the change order's own start→end diff is not drawn anywhere in
rollout mode, though the tags to compute it exist (F20). Underneath that is the point to decide:
by the time the change order exists, the change has *already been applied* to the base — CI
wrote it — so there is nothing to approve here, only to inspect. Two ways to go: (a) keep the
beat as "read what was declared" — an audit view, with the real approvals happening per stage —
which only needs F20; or (b) make the base itself a gated stage — CI opens a ChangeSet, the
change sits pending on the base until someone closes it, and the change order is cut from that
boundary — which is a product design change (F22). Chapter 1 is written for (a).

### 4. What will change if we promote? Bases first, then dev

The workflow's first stage is not a cluster. It is **bases**: the three class bases, dev, test
and prod, that every cluster of that class clones from. Select it. Nothing has been promoted yet,
and the pane says so, but it also shows **What this promotes here**: the diff the promotion
*would* make on each base, computed against that base's current configuration, not copied from
the root.

Read the three side by side. All three take both changes: the image, and the memory limit from
512Mi to 1Gi. A class base is the fleet default for its class, and the fleet default is exactly
what the pipeline meant to raise. `Gates on this stage` reads *1 of 1 satisfied*: the only
requirement is that the root has the change, and it does. Click **Promote**.

Now select **dev**, the one dev cluster. The same preview, one level down: image and memory,
from its class base. Click **Promote**. The dev cluster's configuration takes the change: a new
revision on its `api` unit, carrying the pipeline's description and the change order's name, so
that anyone reading this cluster's history later sees *why* the image moved, not only that it did.

**Gaps.** None in the product: the preview is a real server dry run of the promotion —
`rolloutChanges.ts` diffs the dry-run result against the space's current data — so it honours
protection and function replay. One presenter note: the CLI's `-o mutations` preview simply
omits a kept field; only the UI draws it as kept.

### 5. Promoted is not shipped

Select **test**. The pane refuses: `Gates on this stage` reads *1 of 2 satisfied*, and the one
that fails is *released* — `us-east-dev1` has taken the change but has not released it.

This is the first gate doing its job. Promoting changes configuration; a **release** is the
published bundle that the delivery system actually deploys. The workflow says test may not take
a change that dev has merely staged. Somebody has to decide dev is good.

Go back to **dev** and click **Release**. A release of `us-east-dev1` is published, pinned to the
change order: every unit in the space at the revision the change order marks, including the ones
it did not change. Argo picks it up.

Back on **test**, the gate is green: *2 of 2 satisfied*. Notice what test did *not* ask for: whether
dev is healthy. Dev is where things are allowed to break; the workflow only insists that what
reaches test is something someone chose to release. Prod will be stricter. Different stages,
different bars, one definition.

**Gaps.** *Product:* the UI's **Release** publishes the space's head (`publishRelease` with an
empty request), not `--revision ChangeOrder:<co>` as the CLI does. Identical here because
nothing else has moved, but the two disagree the moment something has (F20). *Demo:* the
script's appendix loops publish pinned to the change order; keep doing that from the CLI when
it matters.

### 6. Reviewing test: same change, two clusters, one look (1 min)

Test is two clusters. Select **test** and read **What this promotes here** for each: the image,
and the memory limit from 512Mi to 1Gi, on both — the fleet default, arriving where the fleet
default is what runs. Nothing to argue with. The gate that mattered here was the one in step 5;
the review is a confirmation, not a decision.

Click **Promote and release** — one action, since the next gate wants both. Both test clusters
take the change and publish a release.

**Gaps.** *Scenario:* done — test carries no local overrides, so this step stays short. *UI:*
none for this step; the protection story is step 8's now (F23 applies there).

### 7. Prod wants proof

Select **prod**. `Gates on this stage`: *2 of 3 satisfied*. Promoted, yes. Released, yes. The
third is *healthy*, and it is not satisfied — the CLI puts it as `live-status not found for
Variant 'us-east-test2'` — because the delivery system has not yet reported on the new test
releases.

This is the point of the third gate. Promoted and released say what *we* did. Healthy says what
*happened* — read from what Argo last reported about each test cluster, at the moment the gate is
evaluated, never cached from when the button was pressed. Prod waits for reality.

`▶` *Argo reports both test clusters healthy.* (Presenter: `cub demo play argobot-test`.)

The gate turns green on the next refresh. Nobody clicked anything to make that happen.

**Gaps.** *Scenario:* done — catalog-api's deployments seed with no live status, so the gate
reads *not found* until argobot reports, and argobot only reports on a release newer than its
last observation. *Product:* F2 — the gate should require the observation to postdate, or
name, the released revision. Refresh is fine: rollout mode polls every 5 s.

### 8. Prod, and done

Select **prod** and read **What this promotes here** one cluster at a time before clicking
anything. `prod2` and `prod3` read like test did: the image, and the memory limit from 512Mi to
1Gi. `prod1`: the image line — and nothing else. `⚠` Its memory limit shows as **kept at 2Gi**,
drawn with a solid border. Same change order, same stage, two different outcomes.

There is a story behind that number. Under peak traffic six months ago prod1 OOM-killed, and the
team doubled *that cluster's* limit to 2Gi and marked the field *protected*: a local override a
merge must not overwrite. Nobody remembers that today, and nobody has to. Without the protection,
the incoming change — a *raise* from the base's point of view — would have silently *lowered*
prod1 from 2Gi to 1Gi, and the next peak would have found the incident again. The promotion
honours the override on the one cluster that has it and applies the fleet default on the two
that do not, and the reviewer sees exactly which is which. If the override has outlived its
reason, that is a decision to record on the variant — in the component view, **Let merges update
this** — not something a merge should decide.

Click **Promote and release**. Three clusters take the change, each merged against its own
configuration — three replicas and warn-level logging everywhere, 1Gi on two of them, 2Gi kept
on the third — and three releases publish.

`▶` *Argo reports prod healthy.* (Presenter: `cub demo play argobot-prod`.)

Now look at the whole rollout. Every stage has taken the change, every stage has released it,
every stage is healthy, and the workflow's **final** check — released and healthy on the last
stage — holds. The change order reads **Released**, and on the Rollouts list it drops out of the
default view into *completed*. `⚠` In the stage strip this shows as the last stage lit; a
terminal *Final* node is coming.

Open the history of any prod cluster's `api` unit:

```
cub revision list api --space catalog-api-us-east-prod1
```

One revision, described in the pipeline's words, tagged with the change order's start and end.
Six clusters, three stages, two people's decisions and one automated system's verdict, and the
whole trail fell out of the mechanism. Nobody wrote a ticket.

**Gaps.** *Product:* the UI has no notion of the workflow's `final` block. The Rollouts list
calls a rollout *complete* when there is no next stage to promote into — i.e. the moment prod
is promoted, before it is released or healthy — and hides it from the default view right then
(F21); the stage strip has no terminal node to light (F12). The CLI reads it right
(`Completed true` only once `final` holds). Until F12/F21 land, close this step on the CLI, or
on the stage strip with prod lit and the words carrying the rest.

---

### What the demo setup does for this chapter, and what is still missing

Everything the chapter needs from the demo tooling landed on cub-demo branch
`chapter-1-tooling` and was run end to end against the Demo org on 2026-09-02:

- **Scenario.** catalog-api seeds clean, with no change order and no live status, so the
  healthy gate waits for argobot. The fleet default memory limit is 512Mi in every class; one
  cluster, `us-east-prod1`, carries its own 2Gi and protects it (the OOM-incident story), via a
  `perVariant` setting and a `protect` entry narrowed to that variant, so the kept field appears
  exactly once, in prod, and the test stage stays about gates. The blocked rollout is
  cert-manager's, for chapter 2.
- **Plays.** The scenario owns its vocabulary: `plays:` in `workflows.yaml` declares `ship`,
  `argobot-test`, `argobot-prod`, `oom-incident` and `heal-test1`, each a list of steps that are
  shell lines or calls to the tool's four primitives — `bump-image` (read the tag, bump it),
  `invoke` (a function on a unit), `changeorder` (open one under the component's workflow),
  `observe` (play argobot: report on selected spaces whose latest release postdates their last
  observation, or paint an incident). `cub demo play <name>` runs one and prints every cub
  command; `cub demo play` lists them. Nothing is hardcoded, so a second run ships 5.4.0.
  `cub demo reset` is down + up.
- **Workflows are entities** since v0.4.15: the seeder creates one ChangeWorkflow per component
  in the home space, `<component>-workflow`, labelled with the scenario and the component, and
  change orders are created under it by reference.

Two things the seeder taught us, both now built in: protection lives on each unit and a clone
starts without it; and a protection set on a class base *after* its deployments were cloned
leaves them behind their upstream, which a change order then refuses to promote past (the
scenario protects deployments only now, but the ordering is kept for class-wide protections).

What remains is product work, tracked in the feedback tab: the change order's own diff at the
source (F20), the Final node and the list's idea of complete (F12, F21), a protection toggle in
rollout mode (F23), the healthy gate accepting a stale observation (F2), and the base-review
question (F22).

*Next: Chapter 2 — a rollout that is refused, and one that is taken back.*
