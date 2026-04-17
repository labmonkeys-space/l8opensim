## ADDED Requirements

### Requirement: MkDocs site source tree exists under `docs/`

The project SHALL host its documentation source in a `docs/` directory at the repository root, structured into four top-level sections (`index`, `getting-started/`, `ops/`, `reference/`) with the pages enumerated below. The directory SHALL contain only Markdown sources, assets referenced by those Markdown sources, and a `requirements.txt` file pinning the build toolchain.

#### Scenario: `docs/` contains the expected page tree

- **WHEN** the `docs/` directory is inspected
- **THEN** the following files SHALL exist:
  - `docs/index.md`
  - `docs/getting-started/quick-start.md`
  - `docs/getting-started/docker.md`
  - `docs/ops/scaling.md`
  - `docs/ops/network-namespace.md`
  - `docs/ops/flow-export.md`
  - `docs/ops/troubleshooting.md`
  - `docs/reference/architecture.md`
  - `docs/reference/snmp.md`
  - `docs/reference/flow-export.md`
  - `docs/reference/device-types.md`
  - `docs/reference/web-api.md`
  - `docs/reference/cli-flags.md`
  - `docs/reference/resource-files.md`
  - `docs/reference/gpu-simulation.md` (or an equivalent `docs/reference/gpu/` subtree if the GPU content is split during migration)

#### Scenario: `docs/requirements.txt` pins the toolchain

- **WHEN** `docs/requirements.txt` is read
- **THEN** it SHALL contain a version specifier for `mkdocs`
- **AND** it SHALL contain a version specifier for `mkdocs-material`
- **AND** it SHALL NOT contain `mike` or any other multi-version docs tool

### Requirement: `mkdocs.yml` configures a Material-themed site bound to the repository

A `mkdocs.yml` file SHALL exist at the repository root. It SHALL select the `material` theme, enable the feature set needed for the migrated content (navigation tabs, dark-mode toggle, admonitions, code fences and tabs, syntax highlighting, and permalink anchors), and bind the site to the canonical published URL and to the repository's `docs/` source for per-page edit links.

#### Scenario: `mkdocs.yml` selects the Material theme

- **WHEN** `mkdocs.yml` is parsed
- **THEN** `theme.name` SHALL equal `material`

#### Scenario: `mkdocs.yml` enables the required Material / PyMdown features

- **WHEN** `mkdocs.yml` is parsed
- **THEN** the Markdown extensions list SHALL include `admonition`
- **AND** SHALL include `pymdownx.superfences`
- **AND** SHALL include `pymdownx.highlight`
- **AND** SHALL include `pymdownx.tabbed`
- **AND** the TOC configuration SHALL enable `permalink`
- **AND** the theme configuration SHALL enable navigation tabs and a dark-mode toggle

#### Scenario: `mkdocs.yml` binds to the published URL and edit source

- **WHEN** `mkdocs.yml` is parsed
- **THEN** `site_url` SHALL equal `https://labmonkeys-space.github.io/l8opensim/`
- **AND** `edit_uri` SHALL equal `edit/main/docs/`

### Requirement: Docs site publishes to GitHub Pages on push to `main`

A GitHub Actions workflow at `.github/workflows/docs.yml` SHALL build and deploy the MkDocs site to GitHub Pages whenever a commit is pushed to `main`. The workflow SHALL run with the minimum permissions necessary for `mkdocs gh-deploy`, pin third-party actions by commit SHA, and deploy to the repository's `gh-pages` branch.

#### Scenario: Workflow triggers on push to main

- **WHEN** `.github/workflows/docs.yml` is parsed
- **THEN** its `on` section SHALL include `push` with `branches: [main]`

#### Scenario: Workflow permissions include `contents: write`

- **WHEN** `.github/workflows/docs.yml` is parsed
- **THEN** the workflow or job `permissions` block SHALL include `contents: write`

#### Scenario: Third-party actions are pinned by SHA

- **WHEN** `.github/workflows/docs.yml` is parsed
- **THEN** every third-party action reference (e.g. `actions/checkout`, `actions/setup-python`) SHALL be pinned to a 40-character commit SHA, not a tag or branch name

#### Scenario: First deploy creates `gh-pages` branch

- **WHEN** the workflow runs for the first time on `main`
- **THEN** it SHALL invoke `mkdocs gh-deploy --force` (or equivalent) which creates the `gh-pages` branch if it does not exist
- **AND** subsequent runs SHALL force-push to the same branch

#### Scenario: Site is reachable at the published URL

- **WHEN** GitHub Pages has been enabled on the `gh-pages` branch (one-time manual step after first deploy)
- **THEN** `https://labmonkeys-space.github.io/l8opensim/` SHALL serve the rendered site
- **AND** the landing page SHALL be the rendered content of `docs/index.md`

### Requirement: Docs builds run in strict mode and fail on broken links

The Makefile target `docs-build` and the CI workflow SHALL invoke MkDocs with `--strict`, causing the build to fail on warnings such as broken internal links, missing nav entries, or unresolved references.

#### Scenario: `make docs-build` uses `--strict`

- **WHEN** the `docs-build` target in the root `Makefile` is inspected
- **THEN** its recipe SHALL invoke `mkdocs build` with the `--strict` flag

#### Scenario: A broken internal link fails CI

- **WHEN** a commit lands on `main` that introduces a broken relative link within `docs/`
- **THEN** the docs workflow SHALL fail
- **AND** the failure log SHALL identify the offending file and line

### Requirement: Makefile exposes docs-tooling entry points backed by a local virtual environment

The root `Makefile` SHALL provide four targets — `docs-install`, `docs-serve`, `docs-build`, and `docs-clean` — that operate against a local Python virtual environment at `.venv`, so that contributors do not pollute their system Python.

#### Scenario: Targets exist with the expected names

- **WHEN** `make -n docs-install`, `make -n docs-serve`, `make -n docs-build`, and `make -n docs-clean` are run
- **THEN** each invocation SHALL succeed (a target by that name exists)

#### Scenario: `docs-install` creates `.venv` and installs pinned deps

- **WHEN** `make docs-install` runs on a checkout without a pre-existing `.venv`
- **THEN** it SHALL create `.venv`
- **AND** it SHALL install the dependencies listed in `docs/requirements.txt` into that `.venv`

#### Scenario: `docs-serve` starts a local live-reload preview

- **WHEN** `make docs-serve` runs after `docs-install`
- **THEN** it SHALL invoke `mkdocs serve` using the `.venv` interpreter
- **AND** the served site SHALL be reachable on a local port (default `:8000`)

#### Scenario: `docs-clean` removes build artifacts and the venv

- **WHEN** `make docs-clean` runs
- **THEN** the `site/` directory SHALL be removed if present
- **AND** the `.venv` directory SHALL be removed if present

### Requirement: `plans/` directory is retired in favour of `docs/reference/`

All content currently under `plans/` SHALL be migrated into `docs/reference/` (specifically into `docs/reference/gpu-simulation.md` or an equivalent `docs/reference/gpu/` subtree), and the `plans/` directory SHALL be deleted once empty. Git history of the migrated files MUST be preserved via `git mv` where a 1:1 rename applies.

#### Scenario: `plans/` directory no longer exists

- **WHEN** the repository root is inspected after this change lands
- **THEN** no `plans/` directory SHALL exist

#### Scenario: Migrated content is discoverable under `docs/reference/`

- **WHEN** the rendered docs site is browsed
- **THEN** content equivalent to each of `plans/gpu-device-proto-model.md`, `plans/gpu-pollaris-complete-coverage.md`, `plans/gpu-pollaris-parsing-rules.md`, and `plans/nvidia-dcgm-simulation.md` SHALL be reachable through the `Reference` navigation group

#### Scenario: Git history of the migrated files is preserved

- **WHEN** `git log --follow` is run against a migrated page in `docs/reference/`
- **THEN** the log SHALL include commits that originally modified the source file under `plans/`

### Requirement: README is slimmed to a landing page with a prominent link to the docs site

After this change lands, `README.md` SHALL function as a landing page rather than a reference manual. It SHALL link prominently to the docs site, retain only the content enumerated in the design document (tagline, badges, pitch, fork notice, single quick start, five marquee features, status, contributing, license), and SHALL NOT contain the reference material that has been migrated into `docs/`.

#### Scenario: README length is within the landing-page budget

- **WHEN** `wc -l README.md` is executed
- **THEN** the line count SHALL be less than or equal to 220

#### Scenario: README links prominently to the docs site

- **WHEN** `README.md` is rendered
- **THEN** a link to `https://labmonkeys-space.github.io/l8opensim/` SHALL appear above or within the first screenful of content (before the features / quick-start block ends)
- **AND** the link text or surrounding copy SHALL identify the target as the project's documentation

#### Scenario: Reference content has been removed from README

- **WHEN** `README.md` is inspected
- **THEN** it SHALL NOT contain a full CLI flag table (which now lives in `docs/reference/cli-flags.md`)
- **AND** SHALL NOT contain a full device-type enumeration (which now lives in `docs/reference/device-types.md`)
- **AND** SHALL NOT contain a full REST API reference (which now lives in `docs/reference/web-api.md`)
- **AND** SHALL NOT contain a full resource-file JSON example (which now lives in `docs/reference/resource-files.md`)

#### Scenario: README retains landing-page content

- **WHEN** `README.md` is inspected
- **THEN** it SHALL contain a title and tagline
- **AND** SHALL contain badges
- **AND** SHALL contain a one-paragraph "what this is" pitch
- **AND** SHALL contain a single-command quick start
- **AND** SHALL contain five marquee feature bullets, each linking into a page under `docs/`
- **AND** SHALL contain a status / scale statement
- **AND** SHALL contain a contributing pointer
- **AND** SHALL contain a license line
