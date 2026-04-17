## Context

`README.md` is the primary landing page for the repository — it's what every new user and evaluator sees first. Today it's 855 lines / ~44 KB and has accumulated three distinct classes of problems:

1. **Factual errors that break onboarding.** The quick-start clone URL (`git clone https://github.com/saichler/opensim.git`, L43) points at a repo that does not exist. The project structure tree (L640–710) shows `resources/` nested under `go/simulator/`, but the actual tree has `resources/` at the repository root.
2. **Identity drift.** The README title says "OpenSim", the repo slug is `l8opensim`, the Makefile publishes `ghcr.io/labmonkeys-space/l8opensim`, the L8 Dockerfile tags `saichler/opensim-web`, and there is no explicit statement that this repository is a fork of `saichler/l8opensim`. A reader cannot unambiguously answer "what is this project called?" or "who owns it?" from the README alone.
3. **Structural drift.** "28 device types" is restated three times (L12, L530, L594) with overlapping tables; troubleshooting is split into two disconnected sections (L196 flow, L754 general); two hero logos stack at the top; there's no DCO/fork-PR guidance for contributors; and there are no status/scale signals (what works, what's experimental, tested at what scale).

A separate in-flight change, `detach-fork-rename-simwerk`, plans to rename the repository to `simwerk` and detach it from the upstream fork. That rename has not shipped. Phase 1 describes **today's** state: the repo is `labmonkeys-space/l8opensim`, it is a fork of `saichler/l8opensim`, and PRs target the fork. When the rename lands, phase 2 (or the rename change itself) will rebrand the README again.

This is explicitly phase 1 of a two-phase split. Phase 2 (issue #45) stands up a MkDocs site and slims the README to ~200 lines once reference material has an off-README home. Phase 1 must be independently mergeable before #45 so the factual bleeding stops first.

Stakeholders: new users (primary — they copy-paste the quick start); contributors (need correct DCO + fork PR policy); evaluators (need accurate identity and scale claims); maintainers (benefit from a canonical device-type section instead of three).

## Goals / Non-Goals

**Goals:**
- A user copy-pasting the quick-start lands on `labmonkeys-space/l8opensim`, not a 404.
- Repository identity is unambiguous: name, fork relationship, and canonical Docker image names are all stated explicitly and consistently.
- The project-structure tree is either accurate or removed (recommended: remove — hand-maintained trees rot and GitHub renders the real one).
- Device-type information appears exactly once in canonical form.
- Troubleshooting is discoverable from one place (single section, or cross-linked).
- Contributors can discover DCO sign-off and fork-PR target from the README.
- Readers still get a complete manual — no reference content moves off the README in this phase.

**Non-Goals:**
- Moving CLI flag reference, API tables, device tables, protocol details, or resource JSON schema to a docs site (deferred to phase 2 / #45).
- Slimming the feature list from 21 bullets to a marquee set (phase 2).
- Hitting a ~200-line README target (phase 2).
- Any logo redesign beyond dropping one duplicate.
- Any code, Makefile, CI, or Dockerfile changes. This is documentation-only.
- Renaming the repo or detaching the fork (owned by `detach-fork-rename-simwerk`).

## Decisions

### D1: Delete the project-structure tree instead of fixing it

**Alternatives considered:**
- (a) Fix the `resources/` path and keep the tree.
- (b) Delete the tree entirely.
- (c) Replace with an auto-generated tree (e.g., `tree -L 2`) committed via CI.

**Chosen: (b) — delete.**

**Rationale:** Any hand-maintained tree rots on the first refactor. GitHub's file browser already renders the live tree directly under the README on the repo homepage. Option (c) adds CI complexity for content that's already one click away. A short "Package layout" prose paragraph with links to the three top-level directories (`go/`, `resources/`, `docs/` if present) gives readers the same orientation without a stale ASCII diagram.

### D2: Canonical identity form is `l8opensim (OpenSim)`

**Alternatives considered:**
- (a) Title `OpenSim` with an aside noting the repo is `l8opensim`.
- (b) Title `l8opensim` only.
- (c) Title `l8opensim (OpenSim)` so both names are searchable and the relationship is obvious.

**Chosen: (c).**

**Rationale:** Users search for both. The repo slug is `l8opensim`, but existing docs, issues, and the upstream project use "OpenSim". Parenthetical form signals the relationship without committing to a rename (which is the rename change's job). When `detach-fork-rename-simwerk` lands, that change owns the next identity update.

### D3: Fork notice is a one-line banner, not a section

**Alternatives considered:**
- (a) Dedicated "## Fork Status" section.
- (b) One-line italic banner under the title.
- (c) Badge only.

**Chosen: (b).**

**Rationale:** A section is heavier than the information warrants. A badge alone is missable. One line under the title is both prominent and minimal. Exact text: *"Fork of [saichler/l8opensim](https://github.com/saichler/l8opensim); PRs target this fork — use `gh pr create --repo labmonkeys-space/l8opensim`."*

### D4: Docker image disambiguation uses explicit mapping

**Alternatives considered:**
- (a) Pick one image and delete mentions of the other.
- (b) Add a short "Container images" subsection that maps each image to its component.
- (c) Leave both mentions, add a footnote.

**Chosen: (b).**

**Rationale:** Both images are real: `ghcr.io/labmonkeys-space/l8opensim:latest` is the simulator (Makefile) and `saichler/opensim-web:latest` is the L8 web frontend (built locally via `docker build` in `go/l8/`). They serve different components. A 2-row table under a "Container images" heading states which is which. Option (a) would silently lose a real artifact; option (c) buries the information.

### D5: Device-type canonical section location

**Alternatives considered:**
- (a) Keep the Features-area mention (L12), kill L530 and L594.
- (b) Kill the Features-area mention, keep a dedicated "Device Types" section with the full table.
- (c) One-line summary in Features (`Supports 28 device types across 8 categories — see Device Types below`), full table in a single dedicated section.

**Chosen: (c).**

**Rationale:** Features is a summary surface — it should tease, not enumerate. The dedicated section is where the full table belongs. One forward reference from Features to that section avoids duplication while keeping the feature list informative.

### D6: Troubleshooting — cross-link rather than merge

**Alternatives considered:**
- (a) Merge flow troubleshooting into the general troubleshooting section.
- (b) Keep both sections, add bidirectional cross-links.
- (c) Move all troubleshooting to a single section near the end.

**Chosen: (b).**

**Rationale:** Flow troubleshooting is tightly coupled to the "Flow Export" section and benefits from locality — a reader configuring flows wants the troubleshooting right there. General troubleshooting covers TUN/netns/permissions issues that apply to basic bring-up. Merging would force readers to context-switch. Cross-links (`See also: [General Troubleshooting](#...)` and vice versa) preserve both uses.

### D7: Status & Scale placement

**Alternatives considered:**
- (a) Top of README, under badges.
- (b) After Features, before Quick Start.
- (c) Near the end alongside contributing/license.

**Chosen: (b).**

**Rationale:** A reader evaluating whether to try the project wants to know "is this stable? how big can I go?" before they run `go build`. After Features but before Quick Start gives evaluators the signal without blocking users who just want to get started.

### D8: Scope discipline — no content migration in phase 1

Any temptation to start moving the CLI flag reference, API tables, or device tables into subfiles is deferred to phase 2. Phase 1 edits happen entirely within `README.md`. This keeps the diff small, reviewable, and independently mergeable, and avoids creating an awkward half-migrated state if phase 2 slips.

## Risks / Trade-offs

- **[Risk] Rename change lands first and makes phase 1 edits stale.** → Mitigation: Phase 1 edits explicitly describe today's identity (`l8opensim`, fork of `saichler/l8opensim`). If the rename lands first, the rename change's tasks already include README rebranding — phase 1's fork notice and clone URL will be superseded cleanly rather than conflict.
- **[Risk] Dropping the project-structure tree removes signal for readers who liked it.** → Mitigation: Replace with a one-paragraph "Package layout" that names the three top-level directories and links to them. GitHub renders the full live tree on the repo homepage anyway.
- **[Risk] The "28 device types" canonicalization miscounts if device resources change during review.** → Mitigation: State the canonical section uses the current count at time of writing and cite `resources/` as the source of truth; future additions update one place.
- **[Risk] Badges at the top break if workflows/names change later.** → Mitigation: Use standard shields.io patterns tied to repo slug and workflow names; document in `specs/readme-landing/spec.md` that badges must reflect the current repo slug so they get updated on rename.
- **[Trade-off] Keeping the README long (phase 1 does not slim).** → Accepted — phase 2 handles slimming once docs site exists. Premature trimming without a home for the removed content would degrade the manual.
- **[Trade-off] Cross-linking troubleshooting instead of merging leaves a minor discoverability gap.** → Accepted — the locality benefit of flow troubleshooting next to flow config outweighs a single cross-link hop.

## Migration Plan

Documentation-only change, no runtime migration. Deployment is the merge itself. Rollback is a `git revert` of the README commit. No users, CI, or image consumers are affected.

## Open Questions

- Should the Status & Scale section cite a specific commit/version for the "tested at 30k devices" claim, or is a prose statement sufficient? (Leaning prose; specific benchmarks belong in phase-2 docs site.)
- Web UI screenshot — is there a current screenshot worth embedding, or leave the logo slot empty? (Defer to implementation; either is acceptable.)
