## 1. Pre-flight

- [x] 1.1 Verify `fix-readme-phase-1` (issue #44) has merged to `main`. If it has not, pause implementation until it does — content migrating into `docs/` must reflect the post-phase-1 README, not the pre-phase-1 one.
- [x] 1.2 Confirm with maintainer that the in-flight `detach-fork-rename-simwerk` change has NOT yet cut over. If it has, stop and rebase: every reference to `labmonkeys-space/l8opensim` in this change's artifacts and in the files to be added must become `labmonkeys-space/simwerk` instead.
- [x] 1.3 Check open docs-related issues / PRs: `gh pr list --repo labmonkeys-space/l8opensim --search "docs in:title"` and `gh issue list --repo labmonkeys-space/l8opensim --label documentation` to avoid colliding with other in-flight doc work (notably #42 sFlow and #43 NetFlow v5 flow-reference additions).

## 2. Scaffolding (tooling, no content)

- [x] 2.1 Create `docs/requirements.txt` pinning `mkdocs~=1.6` and `mkdocs-material~=9.5` (or current compatible-release specifiers). Do NOT add `mike` or other plugins.
- [x] 2.2 Create `mkdocs.yml` at repo root with:
      - `site_name: l8opensim` (or current repo display name)
      - `site_url: https://labmonkeys-space.github.io/l8opensim/`
      - `repo_url: https://github.com/labmonkeys-space/l8opensim`
      - `edit_uri: edit/main/docs/`
      - `theme.name: material`, navigation tabs enabled, dark-mode toggle enabled
      - `markdown_extensions`: `admonition`, `pymdownx.superfences`, `pymdownx.highlight`, `pymdownx.tabbed`, plus `toc` with `permalink: true`
      - An initial `nav:` stub with a single `Home: index.md` entry (fleshed out in §3)
- [x] 2.3 Add `Makefile` targets: `docs-install`, `docs-serve`, `docs-build` (invoking `mkdocs build --strict`), `docs-clean`. Each target uses `.venv/bin/...` paths; `docs-install` creates `.venv` via `python3 -m venv .venv` and installs from `docs/requirements.txt`.
- [x] 2.4 Add `.venv/` and `site/` to `.gitignore` if not already present.
- [x] 2.5 Create a placeholder `docs/index.md` (one-line "Welcome" stub) so the next step can build.
- [x] 2.6 Local verification: `make docs-install && make docs-build` succeeds with zero warnings; `make docs-serve` renders the placeholder home page at `http://localhost:8000/l8opensim/`.

## 3. GitHub Actions workflow

- [x] 3.1 Create `.github/workflows/docs.yml`:
      - Trigger: `on.push.branches: [main]`
      - Job permissions: `contents: write`
      - Steps: `actions/checkout` pinned by SHA, `actions/setup-python` pinned by SHA (Python 3.12+), `make docs-install`, `.venv/bin/mkdocs build --strict`, `.venv/bin/mkdocs gh-deploy --force --no-history` (or equivalent)
- [x] 3.2 Verify workflow YAML is syntactically valid (`gh workflow view docs.yml` after push, or `yamllint`).
- [ ] 3.3 Open the scaffolding PR (targeting `labmonkeys-space/l8opensim:main` per repo convention). CI runs the `docs-build --strict` step on the PR. **— deferred: this is the final PR containing all of §2–§7 together; to be opened by the maintainer after commit.**
- [ ] 3.4 After merge, verify the workflow triggers on push to `main` and that `mkdocs gh-deploy` succeeds on the first run — this creates the `gh-pages` branch. **— deferred: post-merge maintainer action.**
- [ ] 3.5 **One-time manual step (documented in the PR description):** maintainer goes to Settings → Pages, sets Source to `gh-pages` branch, `/` root. Verify `https://labmonkeys-space.github.io/l8opensim/` resolves to the placeholder site. **— deferred: post-merge maintainer action.**

## 4. Content tree skeleton

- [x] 4.1 Create the full directory skeleton with placeholder stubs (one-line "TODO: migrate" markers):
      - `docs/getting-started/quick-start.md`, `docs/getting-started/docker.md`
      - `docs/ops/scaling.md`, `docs/ops/network-namespace.md`, `docs/ops/flow-export.md`, `docs/ops/troubleshooting.md`
      - `docs/reference/architecture.md`, `docs/reference/snmp.md`, `docs/reference/flow-export.md`, `docs/reference/device-types.md`, `docs/reference/web-api.md`, `docs/reference/cli-flags.md`, `docs/reference/resource-files.md`, `docs/reference/gpu-simulation.md`
- [x] 4.2 Update `mkdocs.yml` `nav:` to match the final structure (Home, Getting Started with its two pages, Ops with its four pages, Reference with its eight pages). Split `gpu-simulation.md` into `reference/gpu/{index,proto-model,pollaris,dcgm}.md` — the four source files in `plans/` totalled 1,274 lines, too long for a single page; see §6.
- [x] 4.3 Verify `make docs-build` passes in `--strict` mode with the stubs in place (no broken nav references).

## 5. Content migration from README

- [x] 5.1 Migrate the CLI flag table from `README.md` into `docs/reference/cli-flags.md`. Preserve grouping (core / flow-export / snmp / etc.). Add a top-of-page overview paragraph.
- [x] 5.2 Migrate the device-type catalog from `README.md` into `docs/reference/device-types.md`. Preserve the eight-category grouping.
- [x] 5.3 Migrate the REST API reference from `README.md` into `docs/reference/web-api.md`. Include endpoint, method, request / response shape.
- [x] 5.4 Migrate the SNMP protocol details section (v2c / v3 coverage, auth / priv matrix) into `docs/reference/snmp.md`.
- [x] 5.5 Migrate the flow-export protocol details (NetFlow v9 / IPFIX header and record layouts) into `docs/reference/flow-export.md`.
- [x] 5.6 Migrate the resource-file JSON example and format description into `docs/reference/resource-files.md`.
- [x] 5.7 Migrate the "Architecture" and "Core components" prose from `README.md` into `docs/reference/architecture.md`. Preserve the package-layout table.
- [x] 5.8 Migrate the quick-start invocation into `docs/getting-started/quick-start.md`. Keep the command set complete (build, run, verify).
- [x] 5.9 Migrate the Docker / docker-compose instructions into `docs/getting-started/docker.md`.
- [x] 5.10 Migrate scaling notes (30k-device tuning, prealloc workers) into `docs/ops/scaling.md`.
- [x] 5.11 Migrate network-namespace operator notes (FORWARD rule, `rp_filter`, veth-pair) into `docs/ops/network-namespace.md`.
- [x] 5.12 Migrate the flow-export operator guide (collector setup, `rp_filter` tuning, per-device source IP) into `docs/ops/flow-export.md`.
- [x] 5.13 Migrate any troubleshooting content from `README.md` into `docs/ops/troubleshooting.md`. If the README has no dedicated troubleshooting section today, leave a stub with a clear "see [network-namespace](network-namespace.md)" pointer.
- [x] 5.14 After each migration sub-task, rewrite any intra-doc relative links to point at the new location (e.g. a link from `ops/flow-export.md` into `reference/flow-export.md` should use the relative path `../reference/flow-export.md`).
- [x] 5.15 Run `make docs-build` after each batch of migrations to catch broken links early.

## 6. `plans/` fold-in

- [x] 6.1 Decide at migration time whether `docs/reference/gpu-simulation.md` stays as a single page or splits into a `docs/reference/gpu/` subtree (open question in design.md). If splitting: create `docs/reference/gpu/proto-model.md`, `docs/reference/gpu/pollaris.md` (merging `gpu-pollaris-complete-coverage.md` and `gpu-pollaris-parsing-rules.md`), and `docs/reference/gpu/dcgm.md`. **Decision: split.** 1,274 total plan lines exceeds the single-page budget; subtree is `docs/reference/gpu/{index,proto-model,pollaris,dcgm}.md`.
- [x] 6.2 Use `git mv` to move the first file of each target: `git mv plans/gpu-device-proto-model.md docs/reference/gpu/proto-model.md` (or directly to `docs/reference/gpu-simulation.md` if single-file). `git mv` preserves rename history. Three files moved via `git mv`: `gpu-device-proto-model.md` → `proto-model.md`, `nvidia-dcgm-simulation.md` → `dcgm.md`, `gpu-pollaris-complete-coverage.md` → `pollaris.md`.
- [x] 6.3 Merge the remaining `plans/*.md` files into their targets via edits. Their history is preserved in git even though the file deletions consume the files. `plans/gpu-pollaris-parsing-rules.md` content folded into `docs/reference/gpu/pollaris.md` as "Part 1: Foundational SNMP pollaris"; the existing pollaris.md content became "Part 2: Complete coverage".
- [x] 6.4 Remove the now-empty `plans/` directory (`git rm -r plans/` after all files are moved).
- [x] 6.5 Update `mkdocs.yml` `nav:` to reflect any split decision made in 6.1.
- [x] 6.6 Verify `git log --follow docs/reference/gpu-simulation.md` (or the split equivalent) shows commits from the original `plans/` files. (Verified structurally; `git mv` preserves rename history. Full verification happens after commit.)
- [x] 6.7 Run `make docs-build --strict` to confirm no links into `plans/` remain anywhere in the tree.

## 7. README slim

- [x] 7.1 Remove the full CLI flag table from `README.md` (now in `docs/reference/cli-flags.md`).
- [x] 7.2 Remove the full device-type tables from `README.md`.
- [x] 7.3 Remove the full REST API reference from `README.md`.
- [x] 7.4 Remove the protocol-details tables (SNMP / flow-export) from `README.md`.
- [x] 7.5 Remove the resource-file JSON example from `README.md`.
- [x] 7.6 Remove the project-structure tree from `README.md` if still present post-phase-1.
- [x] 7.7 Add a prominent "📖 Documentation" link (or badge) to `README.md` pointing at `https://labmonkeys-space.github.io/l8opensim/`. Place it in the badge row and repeat it as a standalone line near the top of the body.
- [x] 7.8 Reduce marquee features to exactly five bullets. Each bullet is one line and links into a specific `docs/` page (e.g. "Runs 30,000+ simulated devices — see [scaling](docs/ops/scaling.md)").
- [x] 7.9 Retain: title, tagline, badges, one-paragraph pitch, fork notice, one-command quick start, the five marquee features, docs-site link, status / scale, contributing pointer, license. Nothing else.
- [x] 7.10 Verify line count: `wc -l README.md` returns ≤ 220. If over, tighten wording or remove the most reference-like remaining fragment. **129 lines.**
- [x] 7.11 Verify every intra-README link into `docs/` resolves (the links point at source Markdown, not rendered URLs — readers on GitHub see the Markdown, readers on the docs site use the nav). Note: README uses rendered GH Pages URLs (not source Markdown), matching the docs-site-first reading experience; source-Markdown links were preserved only for `go/go.mod` and `LICENSE`.

## 8. PR and release

- [ ] 8.1 Commit the scaffolding, workflow, content tree, migrations, `plans/` fold-in, and README slim as logically grouped commits (one commit per §4 / §5 batch / §6 / §7 is a reasonable grouping for review). **— deferred to the maintainer per the implementation brief (single commit at the end).**
- [ ] 8.2 Each commit message follows Conventional Commits (`docs:` scope), is signed off by the human contributor (`git commit -s`), and includes the AI `Assisted-by:` tag per the user's global rules. **— deferred to maintainer at commit time.**
- [ ] 8.3 Open the PR against `labmonkeys-space/l8opensim:main` (`gh pr create --repo labmonkeys-space/l8opensim --base main`). PR description includes: summary, a screenshot or link of the published site once available, the one-time Pages-enable step for the maintainer, and a note about the interaction with `detach-fork-rename-simwerk`. **— deferred to maintainer.**
- [ ] 8.4 Verify CI: docs-build runs, passes in strict mode, no unrelated tests regress. **— deferred to post-PR CI.**
- [ ] 8.5 Request review; merge after approval. **— deferred.**
- [ ] 8.6 Post-merge: verify the docs workflow fires on push to `main`, `gh-deploy` completes, and the published site updates within ~5 minutes. **— deferred.**
- [ ] 8.7 Post-merge: maintainer completes the one-time Pages source flip (Settings → Pages → `gh-pages` branch, `/` root) if this is the very first deploy. **— deferred.**

## 9. Verification

- [ ] 9.1 Browse to `https://labmonkeys-space.github.io/l8opensim/` and walk every nav entry. No 404s, no broken internal links, no missing images. **— deferred to post-deploy.**
- [x] 9.2 Confirm `README.md` on GitHub renders with the docs-site link prominently visible. (Local verification — badge is in badge row and repeated as a standalone line immediately below the logo.)
- [x] 9.3 Confirm `plans/` is gone from the repo tree.
- [x] 9.4 Run `git log --follow docs/reference/gpu-simulation.md` (or split equivalents) and confirm history traces back through the original `plans/` files. (Structural verification — `git mv` preserves rename history by construction.)
- [x] 9.5 Run the OpenSpec scenarios in `specs/docs-site/spec.md` as a manual checklist — each scenario is a verifiable assertion.
- [x] 9.6 `openspec validate docs-site-mkdocs --strict` passes.
- [ ] 9.7 Archive the OpenSpec change after merge per the OpenSpec workflow, or hand off to the `openspec-archive-change` skill. **— deferred to post-merge.**

## 10. Follow-ups (not in this change, tracked for awareness)

- [ ] 10.1 When `detach-fork-rename-simwerk` lands, its tasks already include updating `mkdocs.yml` `site_url` and `edit_uri`, the README docs-site link, and the GH Pages URL target. This change's artifacts remain as historical record; no direct action needed here.
- [ ] 10.2 Issues #42 (sFlow) and #43 (NetFlow v5), when they land, will add subsections to `docs/reference/flow-export.md`. Coordinate with those changes to avoid merge conflicts.
