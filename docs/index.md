# l8opensim

**Layer 8 Data Center Simulator** — SNMP v2c/v3, SSH, and HTTPS REST simulation
at 30,000-device scale, built on Linux TUN interfaces and network namespaces.

This site is the canonical home for reference, operational, and extended content.
The repository [`README`](https://github.com/labmonkeys-space/l8opensim) is the
landing page; everything below lives here.

## Where to go next

<div class="grid cards" markdown>

-   __Getting Started__

    ---

    Build the simulator, bring up a small test fleet, and run it in Docker.

    [:octicons-arrow-right-24: Quick start](getting-started/quick-start.md)

-   __Operations__

    ---

    Scale to 30k devices, tune the `opensim` network namespace, and configure
    flow export.

    [:octicons-arrow-right-24: Scaling](ops/scaling.md) ·
    [:octicons-arrow-right-24: Flow export](ops/flow-export.md) ·
    [:octicons-arrow-right-24: SNMP traps](ops/snmp-traps.md)

-   __Reference__

    ---

    Full CLI flag catalog, REST API, device-type tables, and protocol details.

    [:octicons-arrow-right-24: CLI flags](reference/cli-flags.md) ·
    [:octicons-arrow-right-24: Web API](reference/web-api.md) ·
    [:octicons-arrow-right-24: Device types](reference/device-types.md)

-   __GPU Simulation__

    ---

    NVIDIA DGX/HGX simulation, DCGM OID layout, and the pollaris / parser
    integration plan.

    [:octicons-arrow-right-24: GPU overview](reference/gpu/index.md)

</div>

## What it is

l8opensim simulates thousands of network devices, GPU servers, storage systems,
and Linux servers — each with its own IP address, SNMP listener, SSH server,
HTTPS REST endpoint, and flow exporter. It exists to give network-management
and monitoring software a realistic, large-scale load target without needing
real hardware.

See [Architecture](reference/architecture.md) for the package layout, core
components, and key design decisions.

## Status & scale

**Stable:** SNMP v2c/v3, SSH, HTTPS REST for storage APIs, NetFlow v5/v9/IPFIX,
TUN + namespace isolation, web UI and REST API. **Experimental:** sFlow v5
(synthetic), Layer 8 overlay. **Tested scale:** up to 30,000 concurrent devices
on a single host.

## License

Licensed under the Apache License, Version 2.0. See
[LICENSE](https://github.com/labmonkeys-space/l8opensim/blob/main/LICENSE) for
details.
