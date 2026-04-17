## Context

**Current state (2026-04-17).** Documentation lives in three places:

| Surface | Content | Observations |
|---|---|---|
| `README.md` | ~500 lines post-phase-1 (originally ~855) | Mixes pitch, quick start, full CLI flag table, device-type tables, REST API reference, protocol details, resource JSON example, project structure tree. Reference material dominates. |
| `plans/*.md` | 4 GPU-focused design docs (`gpu-device-proto-model.md`, `gpu-pollaris-complete-coverage.md`, `gpu-pollaris-parsing-rules.md`, `nvidia-dcgm-simulation.md`) | Discoverable only via repo tree listing. No cross-links, no index. |
| `docs/` | Empty directory | Placeholder — created in anticipation of this work. |

The repo has no docs-build tooling, no `mkdocs.yml`, no Pages workflow, and no Makefile targets for docs. `README.md` is currently the only on-ramp for new users.

**Constraints.**
- The repo (today) is `labmonkeys-space/l8opensim`. GitHub Pages for a project repo under a user/org publishes at `https://<org>.github.io/<repo>/`, giving `https://labmonkeys-space.github.io/l8opensim/`.
- A separate OpenSpec change in flight (`detach-fork-rename-simwerk`) renames the repo to `simwerk`. If that lands before or after this change, a follow-up is required to update `site_url`, `edit_uri`, and the GH Pages target URL. This change deliberately does NOT try to be rename-safe — it captures today's state correctly; the rename change is already scoped to update touched surfaces.
- Phase-1 README cleanup (`fix-readme-phase-1`, issue #44) is the content-authority dependency — it fixes factual errors in the README before any of it is migrated into `docs/`. If phase 1 has not yet merged when this change is implemented, implementation pauses until it lands. This is a procedural sequencing concern, not a technical one.
- The fork is pre-1.0 and cuts no releases today. Release-gated docs would mean zero deploys. Push-to-main trigger is the only workable cadence.
- Strict-mode builds catch broken internal links. Given the volume of content being moved and cross-referenced, this is non-optional from day one — silent rot would defeat the purpose of the migration.

**Stakeholders.**
- Maintainer: Ronny Trommer (labmonkeys-space).
- Readers: operators deploying the simulator, integrators writing against the REST API, anyone evaluating the project from the repo page.
- No external documentation consumers outside the repo itself.

## Goals / Non-Goals

**Goals:**
- A published docs site at `https://labmonkeys-space.github.io/l8opensim/` that is the canonical home for reference, operations, and extended content — with navigation, search, code highlighting, and per-page edit links.
- A `README.md` that is a landing page, not a reference manual: tagline, pitch, badges, single quick start, five marquee features linking into docs, status, contributing, license — ~200 lines.
- Zero duplication between README and docs. Reference content lives in `docs/` only; the README points at it.
- The `plans/` directory is retired. Design-history content folds into `docs/reference/gpu-simulation.md` so readers discover it through navigation rather than directory listing.
- Pushes to `main` publish within minutes via a single workflow that fails loudly on broken links.
- Local preview (`make docs-serve`) is one command after `make docs-install`.

**Non-Goals:**
- Multi-version docs. `mike` is not used. If the project cuts versioned releases later, a separate change can add it.
- Custom domain (e.g. `docs.simwerk.io`). The GH Pages default URL is the published target.
- Authoring new content beyond migration and the fold-in of `plans/*.md`. Rewriting a section is fine where the migration exposes a factual gap, but net-new chapters are out of scope.
- Converting sub-READMEs (e.g. `go/simulator/README.md` if any) into MkDocs pages. This change covers the top-level README and `plans/` only.
- Release-gating the deploy. Push-to-main is the trigger; there is no "draft" state published anywhere.
- Rename-safety. The `site_url`, `edit_uri`, and target use `l8opensim` today; the rename change handles the cutover.

## Decisions

### D1. Stack: MkDocs + `mkdocs-material`, no `mike`

**Decision:** Use `mkdocs` with the `mkdocs-material` theme. Pin both in `docs/requirements.txt`. Do NOT add `mike` for multi-version support.

**Rationale:** `mkdocs-material` is the same toolchain used by `no42-org/packyard` (a maintainer-adjacent project), which means zero onboarding cost. The feature set needed — navigation tabs, dark-mode toggle, admonitions, code tabs, superfences, syntax highlighting, permalink anchors on headings — all ship with Material or its bundled PyMdown extensions. No plugins outside the Material ecosystem are needed.

**Alternatives considered:**
- **Sphinx + Furo / Read-the-Docs theme.** Heavier tooling, reST-or-MyST syntax, overkill for ~20 Markdown pages.
- **Docusaurus.** Requires Node toolchain; the repo is Go/Python-adjacent and a Node dep adds a second CI path to maintain.
- **Just GitHub-rendered Markdown under `docs/`.** Free, zero tooling — but no search, no nav tabs, no cross-page edit links, and the reference pages would render as raw directory listings. Defeats the "find things by navigation" goal.
- **`mike` for versioned docs.** Deferred: the project doesn't cut releases yet; versioned docs would have exactly one version for an unknowable amount of time. Easy to add later.

### D2. Publishing target and URL

**Decision:** Publish to the default project-Pages URL `https://labmonkeys-space.github.io/l8opensim/`. Set `site_url` in `mkdocs.yml` to that value. Set `edit_uri` to `edit/main/docs/` (relative to the repo).

**Rationale:** The default GH Pages URL requires no DNS, no TLS setup, and no external dependencies. `site_url` is required for canonical URLs in meta tags and sitemaps; hard-coding today's value is correct for today's repo name. `edit_uri: edit/main/docs/` gives each rendered page an "Edit on GitHub" link that deep-links to the source Markdown.

**Rename implication:** When `detach-fork-rename-simwerk` applies, both `site_url` and `edit_uri` (implicitly via the repo URL) become stale. The rename change's tasks must include updating `mkdocs.yml` and verifying the new GH Pages URL resolves.

**Alternative rejected:** Using a CNAME to a custom domain. Adds DNS + TLS + apex-record complexity for zero benefit at the project's current scale.

### D3. Trigger: push to `main` (not release-gated)

**Decision:** The workflow runs on push to `main` and deploys immediately via `mkdocs gh-deploy --force`.

**Rationale:** The project doesn't cut releases today. A release-gated docs workflow would publish zero times. Push-to-main also matches writer-intent: when docs land on `main`, readers should see them.

**Trade-off:** Every commit to `main` that touches `docs/`, `mkdocs.yml`, or `docs/requirements.txt` will trigger a rebuild. A path filter could limit this, but the build is cheap (~30s) and a dependency bump in `requirements.txt` should re-deploy without code changes. Accept the extra builds.

**Alternative considered:** Deploy only on tagged releases. Rejected per rationale above.

### D4. Strict mode from day one

**Decision:** `make docs-build` runs `mkdocs build --strict`. The same flag applies in the CI workflow (via `gh-deploy`, which accepts `--strict` via `mkdocs build` invocation — or the workflow runs `mkdocs build --strict` as a verify step before `gh-deploy`).

**Rationale:** Broken internal links are silent failures in non-strict mode. A ~20-page migration with cross-references between `ops/*` and `reference/*` is exactly the scenario where drift will happen. Catching it at CI time is cheap; finding it after deploy is not.

**Trade-off:** Strict mode is unforgiving — any typo in a relative link fails the build. That friction is the feature: the build fails loudly while the commit is fresh.

### D5. Content layout (navigation groups)

**Decision:** Three top-level nav groups: `Getting Started`, `Ops`, `Reference`, plus `index.md` at the root.

```
docs/
├── index.md                           # landing: what it is, hero link to quick-start
├── getting-started/
│   ├── quick-start.md                 # single-command demo
│   └── docker.md                      # docker-compose / container flow
├── ops/
│   ├── scaling.md                     # 30k-device scale notes, worker tuning
│   ├── network-namespace.md           # opensim namespace, FORWARD rule, rp_filter
│   ├── flow-export.md                 # operator guide: NetFlow v9 / IPFIX setup
│   └── troubleshooting.md             # common failures, diagnostic commands
└── reference/
    ├── architecture.md                # package layout, core components
    ├── snmp.md                        # protocol coverage, OID lookup internals
    ├── flow-export.md                 # RFCs, encoder details, cache model
    ├── device-types.md                # full 28-device catalog
    ├── web-api.md                     # REST endpoints
    ├── cli-flags.md                   # full flag catalog (from README)
    ├── resource-files.md              # JSON resource format
    └── gpu-simulation.md              # folded from plans/*.md
```

**Rationale:** This mirrors a "task-first, then reference" structure: a new reader goes `index → getting-started`, an operator goes `ops`, a deep-diver goes `reference`. The three-way split also makes phase-1 migration mechanical — each README section sorts naturally into one of the three buckets.

**Flexibility:** `reference/gpu-simulation.md` may grow long enough to warrant splitting (e.g. `reference/gpu/proto-model.md`, `reference/gpu/dcgm.md`). The split decision is a judgement call at migration time; the proposal allows it.

### D6. `plans/*.md` folds into `docs/reference/gpu-simulation.md`

**Decision:** All four files under `plans/` migrate into `docs/reference/gpu-simulation.md` (or a small subtree under `docs/reference/gpu/` if length warrants). The `plans/` directory is deleted once empty.

**Rationale:** The `plans/` docs are GPU-simulation design notes that have aged into reference material. They describe current behaviour, not future intent. Keeping them in `plans/` implies "not yet done" — incorrect. Moving them into the reference section matches their actual status.

**History preservation:** Use `git mv plans/<file> docs/reference/gpu/<file>` for 1:1 moves (git detects renames even without `mv`, but `git mv` is cleaner in the commit log). If the fold produces a merged single file, the history of the first source file survives the rename; the others are consumed as edits. That is acceptable loss — the original files remain accessible via `git log -- plans/`.

**Alternative rejected:** Keeping `plans/` as a loosely-maintained parking lot. Rejected on the "zero duplication / single source of truth" goal.

### D7. `docs/requirements.txt` is the single pin surface

**Decision:** Pin `mkdocs` and `mkdocs-material` with compatible version ranges (e.g. `mkdocs~=1.6`, `mkdocs-material~=9.5`). `docs/requirements.txt` is the authoritative list; CI installs from it; local `make docs-install` installs from it.

**Rationale:** A single file keeps local and CI in lockstep. Without pins, a breaking release of either dep silently breaks the build for whoever pulls latest. Compatible-release specifiers (`~=`) strike a balance between reproducibility and not needing to bump pins on every patch release.

**Alternative rejected:** `pip-tools` / `pyproject.toml` / Poetry. Overkill for two dependencies and a `.venv`.

### D8. Makefile targets

**Decision:** Four new Makefile targets, all operating against a local `.venv`:

- `docs-install` — create `.venv` if missing, `pip install -r docs/requirements.txt`.
- `docs-serve` — run `mkdocs serve` (live-reload) on localhost:8000.
- `docs-build` — run `mkdocs build --strict` into `site/`.
- `docs-clean` — remove `site/` and `.venv`.

**Rationale:** Makefile is the existing build-convention surface. Consistent entry points, no new scripts, no arguments to remember. The `.venv` isolates the Python deps from system Python — the project is Go-first, and polluting system Python would be rude.

**Alternative considered:** Global `pip install`. Rejected — operator's Python environment should not be touched.

### D9. Workflow: `.github/workflows/docs.yml`

**Decision:** One workflow file, triggers on push to `main`, one job:

1. `checkout` (pinned by SHA)
2. `setup-python` (pinned by SHA, Python 3.x current stable)
3. `make docs-install`
4. `mkdocs gh-deploy --force`

Permissions: `contents: write` (required for `gh-deploy` to push to the `gh-pages` branch). The default `GITHUB_TOKEN` with that permission is sufficient — no PAT.

**Rationale:**
- Pinning third-party actions by SHA (not tag) is standard supply-chain hardening for workflows with write permissions.
- `gh-deploy --force` is idempotent for our purposes: it rewrites `gh-pages` on every deploy. We never hand-edit `gh-pages`, so force-push is correct.
- `contents: write` is the minimum permission that makes `gh-deploy` work. No `pages: write` needed — that's for the newer Pages-via-artifacts flow, which we're not using (we're using the classic "publish from a branch" flow).

**Alternative considered:** Use the newer `actions/deploy-pages` (artifact-based) flow. More moving parts (three steps: `configure-pages`, `upload-pages-artifact`, `deploy-pages`), less documentation in the MkDocs ecosystem, requires changing the Pages source to "GitHub Actions" rather than "Branch". Stick with the classic flow for simplicity; revisit if we ever want preview deploys per PR.

### D10. First-deploy manual step

**Decision:** Document in the PR description (and this design) that after the first workflow run succeeds, a maintainer must go to Settings → Pages and set Source to `gh-pages` branch, `/` root. This is a one-time action per repo.

**Rationale:** The `gh-pages` branch does not exist until the first `gh-deploy` creates it. GitHub cannot be pre-pointed at a non-existent branch via API without friction. Accept the one-time manual step.

**Alternative considered:** Seed an empty `gh-pages` branch in the first PR so the Pages setting can be flipped before the first deploy. Net-zero benefit — the deploy still has to happen once before users see the site, and the branch-seed adds a step without removing one.

### D11. Slimmed README contract

**Decision:** README's target length is ~200 lines. Content that MUST remain in the README:
- Title and tagline
- Badges (build, license, docs-site link)
- One-paragraph "what this is" pitch
- Fork notice (upstream-attribution block, inherited from `detach-fork-rename-simwerk` when that lands)
- One single-command quick start (full details in `docs/getting-started/quick-start.md`)
- Five marquee features, each a one-line bullet linking into a `docs/` page
- Prominent "📖 Documentation" link/badge pointing at `https://labmonkeys-space.github.io/l8opensim/`
- Status and scale one-liner
- Contributing pointer (one line to `CONTRIBUTING.md` or the repo's contribution conventions in `CLAUDE.md`)
- License line

Content that MUST NOT remain in the README (must have moved to `docs/` first):
- Full CLI flag tables → `docs/reference/cli-flags.md`
- Full device-type tables → `docs/reference/device-types.md`
- Full REST API reference → `docs/reference/web-api.md`
- Protocol details tables → `docs/reference/snmp.md`, `docs/reference/flow-export.md`
- Resource JSON example → `docs/reference/resource-files.md`
- Project structure tree (if present post-phase-1) → `docs/reference/architecture.md`

**Rationale:** A ~200-line cap is a forcing function against drift. The README is the first impression; it must answer "what is this?", "how do I try it?", and "where do I read more?" — nothing more.

**Verification:** `wc -l README.md` ≤ 220 is the mechanical check (a 10% slack over 200 avoids aggressive re-trimming during review).

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| First workflow run fails because `gh-pages` doesn't exist / Pages isn't configured. | `gh-deploy` creates `gh-pages` on first run regardless of Pages setting. The site is just invisible until the maintainer flips the Pages source. Documented as a one-time manual step in the PR. |
| Strict mode rejects a PR due to a broken link that someone else created in a README change. | Strict-mode failures are surfaced as a PR CI failure with the offending file and line. Contributors fix the link in the same PR. This is the intended behaviour. |
| `mkdocs-material` ships a breaking change in a minor release, CI goes red. | Compatible-release pin (`~=9.5`) prevents minor-version drift. If CI goes red on a fresh install, the fix is a pin bump PR. |
| Rename to `simwerk` happens between this change landing and its follow-up updating `site_url`. | Transient window where `site_url` says `labmonkeys-space.github.io/l8opensim` but the repo is `simwerk`. Acceptable — GitHub Pages redirects briefly, and the rename change's tasks explicitly include the `mkdocs.yml` update. |
| Content migrates incorrectly and the docs site ships with stale or wrong information. | Phase-1 dependency (`fix-readme-phase-1`) lands first to make the source content accurate. Migration is mechanical copy-paste with light rewording — not re-authorship. Strict-mode catches link breaks, not factual drift. |
| README slim loses content that a reader relied on being in the README. | The migration is conservative — every section listed in D11 has a target page. A reader grepping for a flag name on GitHub still finds it via the repo code; and `docs/reference/cli-flags.md` is search-indexed on the site. Link from README lands them there. |
| `plans/` directory deletion loses historical context. | `git log -- plans/` preserves the full history of the deleted files indefinitely. The content itself lives on in `docs/reference/gpu-simulation.md`. |
| Workflow permission scope (`contents: write`) is broader than necessary. | `contents: write` is the minimum permission `gh-deploy` needs. The token is scoped to the workflow run; no long-lived credential. Standard for classic-flow Pages deploys. |
| Local `make docs-serve` port conflict with something on :8000. | Documented in `docs/getting-started/quick-start.md` or a `docs/contributing.md` (out of scope for this change) — `mkdocs serve -a 0.0.0.0:8001` override. Not a blocker. |

## Migration Plan

**Preconditions:** `fix-readme-phase-1` (issue #44) merged. Verify with `openspec show fix-readme-phase-1 --json` or a `git log` check for the README cleanup PR.

**Step sequence:**

1. **Scaffolding.** Add `mkdocs.yml`, `docs/requirements.txt`, `.github/workflows/docs.yml`, and the Makefile targets. Do NOT add any `docs/*.md` content yet. Verify `make docs-install && make docs-build` passes locally on an empty site. (Empty because `mkdocs.yml` lists no pages yet; a single placeholder `docs/index.md` is fine.)
2. **Content tree creation.** Create the empty skeleton of `docs/`: `docs/index.md`, `docs/getting-started/`, `docs/ops/`, `docs/reference/`, each with a placeholder stub. Update `mkdocs.yml` nav. Verify `make docs-build --strict` passes.
3. **Content migration.** Copy each target section from `README.md` into its target `docs/` page. Rewrite relative links. Each migration is a single commit for reviewability (`docs: migrate CLI flag reference`, `docs: migrate device types`, etc.).
4. **`plans/` fold-in.** `git mv plans/<file> docs/reference/gpu/<file>` (or merge into `docs/reference/gpu-simulation.md` directly). Delete `plans/` once empty. One commit.
5. **README slim.** Delete migrated sections from README. Add docs-site badge and prominent link. Verify line count ≤ 220. Single commit.
6. **PR.** Open PR. CI runs: unit tests, lint, and the new `docs-build --strict`. Verify docs build passes. Request review.
7. **Merge.** After merge, the docs workflow triggers. First run creates `gh-pages`. Maintainer flips Pages source → `gh-pages` / `/`. Site is live.
8. **Verification.** Browse to `https://labmonkeys-space.github.io/l8opensim/`. Click through every nav entry. Confirm no 404. Confirm README on GitHub has the docs-site link at the top.

**Rollback:**
- Pre-merge: close the PR.
- Post-merge, pre-Pages-enable: revert the PR; no public surface exists yet.
- Post-Pages-enable: revert the PR + disable Pages OR revert just the README slim to restore the old landing surface. The docs-site branch (`gh-pages`) remains until explicitly deleted — harmless if Pages is disabled.

**Rename interaction:** If `detach-fork-rename-simwerk` applies between implementation start and PR merge, pause and rebase: update `mkdocs.yml` `site_url` and `edit_uri` to the new repo slug, update the README docs-site link, update the GH Pages URL everywhere it appears in this change.

## Open Questions

1. **Split or single-file `gpu-simulation.md`?** Four `plans/*.md` files folded into one page may exceed 2000 lines; a `docs/reference/gpu/` subtree with `proto-model.md`, `pollaris.md`, and `dcgm.md` may be cleaner. Decision at migration time based on actual content length and cross-reference density.
2. **Should the workflow include a PR-preview step?** Not in this change's scope. If writer volume grows, a future change can add `actions/deploy-pages`-based preview environments per PR.
3. **Is `CONTRIBUTING.md` a separate concern?** The slimmed README links to a contributing section. If one doesn't exist yet, a stub at `docs/contributing.md` is acceptable but not required by this change — the README link can point at the relevant section of `CLAUDE.md` as an interim measure.
4. **Badge design for the docs-site link.** `shields.io` custom badge vs. Material's built-in docs badge style. Cosmetic; maintainer preference.
