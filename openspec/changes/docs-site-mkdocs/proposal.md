## Why

Documentation currently lives entirely in `README.md` (~500 lines after phase-1 cleanup, originally ~855) with four additional design docs loose in `plans/`. Reference content — CLI flag tables, device-type tables, REST API listings, protocol details — dominates the landing page and pushes the project pitch below the fold, and `plans/*.md` are discoverable only by directory listing. A published docs site with logical navigation lets the README collapse to a scannable landing page and gives reference content a home with anchors, search, and edit links.

Phase 1 (`fix-readme-phase-1`, issue #44) fixed structural problems in the README in place; phase 2 (this change) stands up the MkDocs + Material site, migrates the reference sections out, folds the `plans/` docs into the reference section, and slims the README to a ~200-line landing page. Acting now avoids paying the migration cost twice: the in-flight sFlow (#42) and NetFlow v5 (#43) work will add subsections that belong under `docs/reference/flow-export.md` rather than the README.

## What Changes

- Add a MkDocs + `mkdocs-material` documentation site rooted at `docs/`, published to `https://labmonkeys-space.github.io/l8opensim/` via GitHub Pages from the `gh-pages` branch.
- Add `mkdocs.yml` at repo root with navigation tabs, dark-mode toggle, `admonition`, `pymdownx.superfences`, `pymdownx.highlight`, `pymdownx.tabbed`, `toc.permalink`, `site_url` pointing at the GH Pages target, and `edit_uri` of `edit/main/docs/`.
- Add `docs/requirements.txt` pinning `mkdocs` and `mkdocs-material` (no `mike` — single-version site).
- Add `Makefile` targets `docs-install`, `docs-serve`, `docs-build` (with `--strict`), and `docs-clean`, all operating inside a `.venv`.
- Add `.github/workflows/docs.yml` that triggers on push to `main`, checks out the tree, runs `setup-python`, `make docs-install`, and `mkdocs gh-deploy --force`. Third-party actions are pinned by SHA. Workflow uses the default `GITHUB_TOKEN` with `contents: write` permission.
- Create the documented content tree under `docs/`: `index.md`, `getting-started/{quick-start,docker}.md`, `ops/{scaling,network-namespace,flow-export,troubleshooting}.md`, `reference/{architecture,snmp,flow-export,device-types,web-api,cli-flags,resource-files,gpu-simulation}.md`. Content migrates from the current README; nothing new is written.
- Fold `plans/gpu-device-proto-model.md`, `plans/gpu-pollaris-complete-coverage.md`, `plans/gpu-pollaris-parsing-rules.md`, and `plans/nvidia-dcgm-simulation.md` into `docs/reference/gpu-simulation.md` (splitting further only if length warrants). Use `git mv` where the mapping is 1:1 to preserve history; delete `plans/` once content lands.
- **BREAKING (for docs consumers)**: Slim `README.md` from ~500 lines to ~200 lines. Remove full CLI flag tables, full device-type tables, full REST API reference, protocol details tables, the resource JSON example, and the project structure tree. Retain tagline, badges, one-paragraph pitch, fork notice, single-command quick start, five marquee features (each linking into `docs/`), a prominent "Documentation" link/badge pointing at the GH Pages URL, status and scale, contributing, and license. Anything removed must live in `docs/` before this change lands.
- One-time manual step (documented in the PR, not automated): after the first successful workflow run creates the `gh-pages` branch, flip GitHub Pages source to that branch in repo settings.

## Capabilities

### New Capabilities

- `docs-site`: The MkDocs-based documentation site — its source tree layout under `docs/`, its build toolchain and tooling pins, its publishing pipeline to GitHub Pages on push-to-main, its strict-mode build invariant, the slimmed README contract (what must stay, what must have migrated), and the retirement of the `plans/` directory in favour of `docs/reference/`.

### Modified Capabilities

<!-- None — no prior OpenSpec specs cover documentation. The existing `project-identity` spec (from change `detach-fork-rename-simwerk`) covers naming surfaces, not documentation structure. -->

## Impact

**In-tree files added**
- `mkdocs.yml` (repo root)
- `docs/requirements.txt`
- `docs/index.md`
- `docs/getting-started/quick-start.md`, `docs/getting-started/docker.md`
- `docs/ops/scaling.md`, `docs/ops/network-namespace.md`, `docs/ops/flow-export.md`, `docs/ops/troubleshooting.md`
- `docs/reference/architecture.md`, `docs/reference/snmp.md`, `docs/reference/flow-export.md`, `docs/reference/device-types.md`, `docs/reference/web-api.md`, `docs/reference/cli-flags.md`, `docs/reference/resource-files.md`, `docs/reference/gpu-simulation.md`
- `.github/workflows/docs.yml`

**In-tree files modified**
- `README.md` — slimmed from ~500 lines to ~200 lines; adds a prominent link/badge to the docs site.
- `Makefile` — new `docs-*` targets.

**In-tree files removed**
- `plans/gpu-device-proto-model.md`
- `plans/gpu-pollaris-complete-coverage.md`
- `plans/gpu-pollaris-parsing-rules.md`
- `plans/nvidia-dcgm-simulation.md`
- `plans/` directory itself (once empty)

**Out-of-tree actions**
- First workflow run creates the `gh-pages` branch; a maintainer then enables Pages in repo settings (Source: `gh-pages` branch, `/` root). One-time, manual.
- Verify GH Pages URL `https://labmonkeys-space.github.io/l8opensim/` resolves after first deploy.

**Dependencies**
- Lands AFTER `fix-readme-phase-1` (issue #44). The in-place README cleanup lands first so the content migrating into `docs/` reflects current reality.
- Does NOT block #42 (sFlow) or #43 (NetFlow v5), but those changes will add subsections to `docs/reference/flow-export.md` once this change lands.
- Parallel change `detach-fork-rename-simwerk` renames the repo to `simwerk`. This change deliberately uses the CURRENT name (`labmonkeys-space/l8opensim`) in `site_url`, `edit_uri`, and the GH Pages target. If the rename lands, a follow-up change updates those three surfaces and moves the Pages site to the new slug.

**Not in scope**
- Authoring new documentation content beyond migration and lightweight folding of `plans/*.md`.
- Adding `mike` or any multi-version docs tooling.
- Publishing to a custom domain.
- Converting READMEs elsewhere in the tree (e.g. `go/simulator/`) into MkDocs pages.
- Release-gating the docs deploy.
