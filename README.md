# l8opensim (OpenSim) — Layer 8 Data Center Simulator

> Fork of [saichler/l8opensim](https://github.com/saichler/l8opensim); PRs target this fork — use `gh pr create --repo labmonkeys-space/l8opensim`.

[![CI](https://github.com/labmonkeys-space/l8opensim/actions/workflows/ci.yml/badge.svg)](https://github.com/labmonkeys-space/l8opensim/actions/workflows/ci.yml)
[![Docs](https://img.shields.io/badge/docs-labmonkeys--space.github.io-blue?logo=readthedocs)](https://labmonkeys-space.github.io/l8opensim/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/labmonkeys-space/l8opensim?filename=go%2Fgo.mod)](https://github.com/labmonkeys-space/l8opensim/blob/main/go/go.mod)
[![License](https://img.shields.io/github/license/labmonkeys-space/l8opensim)](https://github.com/labmonkeys-space/l8opensim/blob/main/LICENSE)
[![Container Image](https://img.shields.io/badge/ghcr.io-l8opensim-blue?logo=docker)](https://github.com/labmonkeys-space/l8opensim/pkgs/container/l8opensim)
[![Latest Release](https://img.shields.io/github/v/release/labmonkeys-space/l8opensim?include_prereleases&sort=semver)](https://github.com/labmonkeys-space/l8opensim/releases)

![OpenSim Logo](opensim.png)

**📖 Documentation: <https://labmonkeys-space.github.io/l8opensim/>**

A scalable network and infrastructure simulator that exposes realistic
SNMP v2c/v3, SSH, and HTTPS REST interfaces for testing network management
software, monitoring systems, and automation tools. OpenSim can simulate
tens of thousands of network devices, GPU servers, storage systems, and
Linux servers — each with its own IP address via Linux TUN interfaces and
network namespaces.

## Highlights

- **Runs 30,000+ simulated devices on a single host** — see [Scaling](https://labmonkeys-space.github.io/l8opensim/ops/scaling/).
- **28 device types across 8 categories** (core / edge routers, DC and
  campus switches, firewalls, servers, NVIDIA DGX/HGX GPU servers, and
  enterprise storage) — see [Device types](https://labmonkeys-space.github.io/l8opensim/reference/device-types/).
- **Multi-protocol per device:** SNMP v2c/v3 (MD5/SHA1 auth, DES/AES128
  privacy), SSH with VT100 terminal emulation, and HTTPS REST — see
  [SNMP reference](https://labmonkeys-space.github.io/l8opensim/reference/snmp/)
  and [Web API](https://labmonkeys-space.github.io/l8opensim/reference/web-api/).
- **Realistic dynamic metrics:** CPU / memory / temperature on 100-point
  sine waves, analytic HC interface counters, per-GPU DCGM metrics — see
  [GPU simulation](https://labmonkeys-space.github.io/l8opensim/reference/gpu/).
- **Per-device flow export** (NetFlow v5 / v9, IPFIX, sFlow v5) with
  per-device source IPs — see
  [Flow export](https://labmonkeys-space.github.io/l8opensim/ops/flow-export/).

## Quick start

```bash
git clone https://github.com/labmonkeys-space/l8opensim.git
cd l8opensim/go/simulator && go build -o simulator .

# Auto-create 5 devices starting at 192.168.100.1
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 5
```

Then query any device:

```bash
snmpget -v2c -c public 192.168.100.1 1.3.6.1.2.1.1.1.0
ssh simadmin@192.168.100.1                            # password: simadmin
```

Full walkthrough: [Getting started → Quick start](https://labmonkeys-space.github.io/l8opensim/getting-started/quick-start/).
Container and Kubernetes deployment: [Getting started → Docker](https://labmonkeys-space.github.io/l8opensim/getting-started/docker/).

## Status & scale

**Stable** — SNMP v2c/v3, SSH, HTTPS REST (storage APIs), NetFlow v5/v9 and
IPFIX, TUN-per-device scaling with `opensim` network-namespace isolation,
web UI, REST control plane.

**Experimental** — sFlow v5 (synthesised from `FlowCache` records with a
fixed `sampling_rate`; suitable for collector-plumbing validation, not
link-utilisation benchmarking — see
[Flow export reference → sFlow caveat](https://labmonkeys-space.github.io/l8opensim/reference/flow-export/#sflow-caveat)),
and the optional Layer 8 (`go/l8/`) vnet overlay + HTTPS proxy.

**Tested scale** — up to 30,000 concurrent simulated devices on a single
host. **Toolchain** — Go 1.26 or later; canonical version pinned in
[`go/go.mod`](go/go.mod).

## Documentation map

The docs site has four top-level sections:

- [Getting Started](https://labmonkeys-space.github.io/l8opensim/getting-started/quick-start/) — build, first run, Docker.
- [Operations](https://labmonkeys-space.github.io/l8opensim/ops/scaling/) — scaling, network namespace, flow export, troubleshooting.
- [Reference](https://labmonkeys-space.github.io/l8opensim/reference/architecture/) — architecture, CLI flags, web API, device types, SNMP, flow export, resource files, GPU simulation.
- [GPU simulation](https://labmonkeys-space.github.io/l8opensim/reference/gpu/) — NVIDIA DCGM OID layout, per-GPU metrics, and the pollaris / parser integration notes (formerly `plans/`).

Reference content that used to live in this README now lives in the docs
site. A bare `README.md` on GitHub is intentional: the site is the canonical
home.

## Contributing

Contributions are welcome. Two project policies apply to every patch:

**1. Sign off every commit (Developer Certificate of Origin).** All commits
must carry a `Signed-off-by:` trailer certifying the
[DCO](https://developercertificate.org/). Use `-s` on every commit:

```bash
git commit -s -m "your commit message"
```

A DCO-check gate will fail any PR whose commits are missing the sign-off
trailer.

**2. Open PRs against this fork, not upstream.** This repository is a fork
of [`saichler/l8opensim`](https://github.com/saichler/l8opensim). PRs must
target `labmonkeys-space/l8opensim` — not the upstream. Use the `--repo`
flag explicitly so `gh` doesn't default to upstream:

```bash
gh pr create --repo labmonkeys-space/l8opensim --base main
```

**Suggested workflow**

1. Fork `labmonkeys-space/l8opensim`.
2. Create a feature branch.
3. Make your changes and add / update tests.
4. Run `make check-tidy && make build && make test` locally.
5. `git commit -s` each commit.
6. `gh pr create --repo labmonkeys-space/l8opensim --base main`.

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for
details.

---

**OpenSim** — simulate networks, test at scale, develop with confidence.
