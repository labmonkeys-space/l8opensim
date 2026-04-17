## ADDED Requirements

### Requirement: Accurate clone instructions

The README SHALL provide a clone command that resolves to the canonical repository at `https://github.com/labmonkeys-space/l8opensim`. No clone command in the README SHALL reference a repository that does not exist or that is not the canonical home of this project.

#### Scenario: Quick-start clone URL is correct

- **WHEN** `README.md` is searched for any occurrence of `git clone https://github.com/`
- **THEN** every match SHALL target `https://github.com/labmonkeys-space/l8opensim.git`
- **AND** no match SHALL target `https://github.com/saichler/opensim.git` or any other non-canonical URL

#### Scenario: Clone command resolves to a real repository

- **WHEN** a reader copy-pastes the quick-start clone command verbatim
- **THEN** `git clone` SHALL succeed against the URL shown in the README
- **AND** the resulting working copy SHALL be the `labmonkeys-space/l8opensim` project

### Requirement: Unambiguous project identity

The README SHALL state the project's canonical name in a form that includes both the repository slug (`l8opensim`) and the commonly used short form (`OpenSim`), so that readers searching for either name recognize the project. The title and any top-level project-name references SHALL be consistent.

#### Scenario: Title contains both names

- **WHEN** the first-level heading of `README.md` is rendered
- **THEN** it SHALL contain both `l8opensim` and `OpenSim`
- **AND** the heading SHALL use the form `l8opensim (OpenSim)` or an equivalent that presents both names

#### Scenario: No conflicting identity claims

- **WHEN** `README.md` is read end-to-end
- **THEN** no prose sentence SHALL claim the project's name is solely `OpenSim` without acknowledging `l8opensim`, and vice versa

### Requirement: Fork relationship disclosure

The README SHALL disclose that this repository is a fork of `saichler/l8opensim` and SHALL state the project's pull-request policy, so contributors do not accidentally open PRs against upstream. This requirement applies for as long as the repository remains a fork; when the fork is severed (under a separate change), this requirement is superseded by that change's identity spec.

#### Scenario: Fork notice appears near the top

- **WHEN** `README.md` is rendered
- **THEN** within the first 25 rendered lines after the title, a notice SHALL appear that identifies the project as a fork of `saichler/l8opensim`
- **AND** the notice SHALL link to `https://github.com/saichler/l8opensim`

#### Scenario: PR target policy is stated

- **WHEN** the fork notice or the Contributing section is read
- **THEN** it SHALL state that pull requests target `labmonkeys-space/l8opensim` rather than the upstream fork
- **AND** it SHALL show the explicit invocation `gh pr create --repo labmonkeys-space/l8opensim` at least once

### Requirement: Container image mapping is explicit

The README SHALL disambiguate the two container images referenced in this repository by mapping each image name to the component it represents. A reader SHALL be able to determine which image corresponds to the simulator and which corresponds to the L8 web frontend.

#### Scenario: Both images are identified

- **WHEN** `README.md` is searched for container-image references
- **THEN** `ghcr.io/labmonkeys-space/l8opensim` SHALL be identified as the simulator image
- **AND** `saichler/opensim-web` SHALL be identified as the L8 web frontend image
- **AND** the mapping SHALL appear in a single dedicated "Container images" subsection or equivalent, not scattered across unrelated sections

### Requirement: Project-structure tree is accurate or absent

The README SHALL NOT contain a hand-maintained project-structure tree whose paths do not match the actual filesystem. If a structure overview is present, every path it shows SHALL exist at the stated location relative to the repository root.

#### Scenario: No structure tree with wrong paths

- **WHEN** `README.md` is rendered
- **THEN** either no ASCII or Markdown-rendered project-structure tree is present, or every path shown in such a tree SHALL correspond to an actual path in the repository
- **AND** in particular, `resources/` SHALL NOT be shown as a subdirectory of `go/simulator/`

#### Scenario: Prose orientation is acceptable

- **WHEN** the README omits a structure tree
- **THEN** a short prose "Package layout" description MAY substitute, naming the top-level directories (`go/`, `resources/`, and any others) without claiming a complete file listing

### Requirement: Device types appear in one canonical section

The README SHALL contain exactly one canonical section that enumerates supported device types. Other places in the README MAY summarize the count (e.g., "28 device types across 8 categories") and MAY link to the canonical section, but MUST NOT re-enumerate the full list.

#### Scenario: Full device table appears once

- **WHEN** `README.md` is searched for tables or lists enumerating individual device types
- **THEN** exactly one such enumeration SHALL be present
- **AND** that enumeration SHALL be the "Device Types" section (or equivalently named canonical section)

#### Scenario: Feature-list mention forwards to canonical section

- **WHEN** the Features list mentions device-type counts
- **THEN** it SHALL do so in a single summary line
- **AND** it SHALL link to the canonical Device Types section rather than re-list the types

### Requirement: Troubleshooting is discoverable from a single entry point

The README SHALL make troubleshooting information discoverable either by consolidating all troubleshooting into a single section, or by cross-linking any topic-specific troubleshooting subsections (e.g., flow troubleshooting) to and from a general troubleshooting section.

#### Scenario: Flow troubleshooting cross-links to general troubleshooting

- **WHEN** the Flow Export troubleshooting subsection is read
- **THEN** it SHALL contain a link to the general Troubleshooting section

#### Scenario: General troubleshooting cross-links to flow troubleshooting

- **WHEN** the general Troubleshooting section is read
- **THEN** it SHALL contain a link to the Flow Export troubleshooting subsection

### Requirement: Contributor policy section

The README SHALL include a Contributing section that states two specific project policies: the DCO sign-off requirement and the fork-target PR policy. The section SHALL show the explicit command forms required to comply with each policy.

#### Scenario: DCO sign-off is documented

- **WHEN** the Contributing section is read
- **THEN** it SHALL state that commits require a Developer Certificate of Origin sign-off
- **AND** it SHALL show `git commit -s` as the required command form

#### Scenario: Fork PR target is documented

- **WHEN** the Contributing section is read
- **THEN** it SHALL state that pull requests are opened against `labmonkeys-space/l8opensim`
- **AND** it SHALL show `gh pr create --repo labmonkeys-space/l8opensim` as the required command form

### Requirement: Status and scale section

The README SHALL include a Status & Scale section (or equivalently named section) that states which features are stable versus experimental, the tested scale, and the minimum Go toolchain version. This section SHALL appear before the Quick Start section so evaluators see it before running build commands.

#### Scenario: Section states stable vs. experimental

- **WHEN** the Status & Scale section is read
- **THEN** it SHALL identify at least one set of features as stable and at least one as experimental (or explicitly state that all features are stable if that is accurate)

#### Scenario: Section states tested scale

- **WHEN** the Status & Scale section is read
- **THEN** it SHALL state the tested device-count scale (currently 30,000 devices)

#### Scenario: Section states Go toolchain requirement

- **WHEN** the Status & Scale section is read
- **THEN** it SHALL state the minimum Go version required to build the simulator (currently Go 1.26 or later)

#### Scenario: Section placement

- **WHEN** `README.md` is rendered
- **THEN** the Status & Scale section SHALL appear before the Quick Start / Build & Run section

### Requirement: Badges at the top

The README SHALL display a row of status badges near the top (above or immediately below the title/fork-notice block) covering, at minimum, CI status, Go version, license, container image, and latest release.

#### Scenario: Required badges present

- **WHEN** `README.md` is rendered
- **THEN** within the first 10 rendered lines after the title, badges SHALL be present for each of: CI build status, Go version, License, GHCR container image, latest release
- **AND** each badge SHALL link to the corresponding source of truth (workflow runs, `go.mod`, `LICENSE`, registry page, releases page)

### Requirement: Single hero image

The README SHALL include at most one hero image (logo or screenshot) at the top. The two currently stacked logo files (`opensim.png` and `opensim1.png`) SHALL NOT both appear in the rendered README.

#### Scenario: At most one hero image

- **WHEN** `README.md` is rendered
- **THEN** at most one `<img>` or Markdown image reference SHALL appear before the first second-level heading
- **AND** `opensim.png` and `opensim1.png` SHALL NOT both be referenced above that heading
