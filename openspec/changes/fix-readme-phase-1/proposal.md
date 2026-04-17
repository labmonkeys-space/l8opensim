## Why

`README.md` (855 lines / ~44 KB) contains factual errors that actively harm new-user onboarding: the clone URL points at a non-existent repo (`saichler/opensim`), the project structure tree shows `resources/` nested under `go/simulator/` when it actually lives at the repo root, and the repository identity is muddled (title "OpenSim", repo name `l8opensim`, Docker image `saichler/opensim-web` vs. Makefile's `ghcr.io/labmonkeys-space/l8opensim`). A new user copy-pasting the quick-start currently ends up on the wrong repository.

This is phase 1 of a two-phase docs cleanup. Phase 2 (#45) will stand up a MkDocs site and slim the README further; phase 1 corrects the bleeding and tightens structure so the README works as a landing page in the interim. Phase 1 must be independently mergeable ahead of #45.

## What Changes

**Factual / breaking fixes (required):**
- Clone URL: replace `https://github.com/saichler/opensim.git` (L43) with `https://github.com/labmonkeys-space/l8opensim.git`.
- Repository identity: reconcile the title "OpenSim" with the repo name `l8opensim`. Adopt one canonical form (recommended: title `l8opensim (OpenSim)` so both names are searchable).
- Fork notice: add a one-line fork banner near the top — "Fork of [saichler/l8opensim](https://github.com/saichler/l8opensim); PRs target this fork."
- Project structure tree (L640–710): **delete** the tree. The `resources/` path is wrong (shown under `go/simulator/`, actually at repo root) and any hand-maintained tree will rot. GitHub's tree view already renders this.
- Docker image naming: reconcile `saichler/opensim-web:latest` (L801, used by L8 web frontend) with `ghcr.io/labmonkeys-space/l8opensim:latest` (Makefile, simulator image). Call out which image maps to which component.

**Structural tightening:**
- Add badges at the top: CI / Go version / License / GHCR image / latest release.
- Collapse the three overlapping "28 device types" sections (L12, L530, L594) into a single canonical section.
- Cross-link or merge General Troubleshooting (L754) and Flow Troubleshooting (L196).
- Remove one of the two stacked hero logos (`opensim.png`, `opensim1.png`); optionally replace with a Web UI screenshot.
- Expand Contributing section with DCO sign-off (`git commit -s`) and the fork PR policy (`gh pr create --repo labmonkeys-space/l8opensim`).
- Add a Status & Scale section: stable vs. experimental features, tested scale (30k devices), Go 1.26+ requirement.

**Out of scope (phase 2 / #45):**
- Moving reference content (full CLI flags, API tables, device tables, protocol details, resource JSON schema) into a docs site.
- Slimming the feature list from 21 bullets to 5 marquee items.
- Final README target length (~200 lines once the docs site is live).

## Capabilities

### New Capabilities
- `readme-landing`: Rules for what `README.md` must contain to function as a landing page — accurate clone instructions, unambiguous project identity, fork relationship disclosure, contributor policy (DCO + fork PR target), and status/scale signalling. Future README edits (including phase 2 slimming) modify this spec.

### Modified Capabilities
<!-- None — `project-identity` is owned by the in-flight detach-fork-rename-simwerk change and is not yet merged. Phase 1 README fixes describe TODAY's identity (fork of saichler/l8opensim, labmonkeys-space/l8opensim) and do not modify that future spec. -->

## Impact

**In-tree files touched**
- `README.md` — the only code/content file modified by this change.

**Not touched**
- No Go source, no Makefile, no Dockerfile, no CI config, no `CLAUDE.md`. Phase 1 is documentation-only.

**Downstream consumers**
- No API, module path, image tag, or binary changes. Existing clones, CI pipelines, and `docker pull` invocations continue to work.

**Coordination with other in-flight changes**
- The `detach-fork-rename-simwerk` change is not yet merged; the repository is still `labmonkeys-space/l8opensim` and still a fork of `saichler/l8opensim`. Phase 1 documents today's state. When the rename lands, the `project-identity` spec from that change will govern identity updates and the README will be rebranded again under that change's tasks.

**Out of scope**
- Any content migration to a docs site (deferred to #45).
- Slimming the feature list.
- Removing or relocating reference tables.
- Logo artwork changes beyond dropping a duplicate (a real logo redesign is out of scope).
