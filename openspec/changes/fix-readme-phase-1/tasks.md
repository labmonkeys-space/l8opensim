## 1. Factual fixes (required)

- [x] 1.1 Replace clone URL `https://github.com/saichler/opensim.git` with `https://github.com/labmonkeys-space/l8opensim.git` in the Quick Start / Build & Run section (current L43) and anywhere else it appears
- [x] 1.2 Update the first-level heading to the canonical form `l8opensim (OpenSim)` (or an equivalent form presenting both names)
- [x] 1.3 Add a one-line fork notice under the title: *"Fork of [saichler/l8opensim](https://github.com/saichler/l8opensim); PRs target this fork — use `gh pr create --repo labmonkeys-space/l8opensim`."*
- [x] 1.4 Delete the project-structure tree at current L640–710
- [x] 1.5 Replace the deleted tree with a short "Package layout" prose paragraph naming top-level directories (`go/`, `resources/`) and linking to them in the GitHub tree view
- [x] 1.6 Add a "Container images" subsection that maps `ghcr.io/labmonkeys-space/l8opensim` → simulator (Makefile) and `saichler/opensim-web` → L8 web frontend (`go/l8/Dockerfile`)

## 2. Structural tightening

- [x] 2.1 Add a badges row near the top covering CI, Go version, License, GHCR container image, and latest release; verify each link resolves
- [x] 2.2 Remove the duplicate hero logo — keep one of `opensim.png` / `opensim1.png`, not both (optionally replace with a Web UI screenshot if available)
- [x] 2.3 Consolidate the three device-type mentions (current L12, L530, L594) into a single canonical "Device Types" section; replace the Features-area mention with a one-line summary that links to the canonical section
- [x] 2.4 Verify the canonical Device Types section reflects the current count of device types and categories (source of truth: `resources/` directory)
- [x] 2.5 Add bidirectional cross-links between the Flow Export troubleshooting subsection (current L196) and the General Troubleshooting section (current L754)
- [x] 2.6 Expand the Contributing section to state the DCO sign-off requirement with the `git commit -s` command shown explicitly
- [x] 2.7 Expand the Contributing section to state the fork PR policy with `gh pr create --repo labmonkeys-space/l8opensim` shown explicitly
- [x] 2.8 Add a "Status & Scale" section before Quick Start covering: stable vs. experimental features, tested scale (30,000 devices), and Go toolchain minimum (Go 1.26 or later)

## 3. Verification

- [x] 3.1 Grep the README for `saichler/opensim.git` and confirm zero matches
- [x] 3.2 Grep the README for `saichler/opensim` (without `.git`) and confirm the only remaining matches are the legitimate `saichler/opensim-web` image reference and the fork-of attribution link
- [x] 3.3 Confirm the README shows only one `git clone` command and it targets `labmonkeys-space/l8opensim`
- [x] 3.4 Confirm no ASCII or Markdown-rendered project-structure tree remains, or every path in any remaining tree exists on disk
- [x] 3.5 Confirm exactly one full device-type enumeration exists in the README; all other mentions are summary lines that link to it
- [x] 3.6 Confirm the fork notice appears within the first 25 rendered lines after the title and links to `https://github.com/saichler/l8opensim`
- [x] 3.7 Confirm all five required badges (CI, Go version, License, GHCR, latest release) are present within the first 10 rendered lines after the title and each links to its source of truth
- [x] 3.8 Confirm the Status & Scale section appears before the Quick Start section
- [x] 3.9 Confirm at most one hero image appears above the first second-level heading
- [x] 3.10 Render the README locally or via GitHub preview and visually check: fork notice visible, badges render, cross-links resolve, TOC (if present) still matches section order

## 4. Close out

- [ ] 4.1 Open PR against `labmonkeys-space/l8opensim` with `gh pr create --repo labmonkeys-space/l8opensim --base main` linking to issue #44
- [ ] 4.2 Note in the PR description that this is phase 1 of a two-phase cleanup and that phase 2 (#45) handles docs-site migration and further slimming
- [ ] 4.3 Note in the PR description that the fork notice and clone URL describe today's state and will be superseded cleanly when `detach-fork-rename-simwerk` lands
