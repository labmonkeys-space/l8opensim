# Docker

l8opensim ships two distinct container images built through different
pipelines. Pick the one that matches what you're doing:

| Image | Component | Built by | Published to |
|-------|-----------|----------|--------------|
| `ghcr.io/labmonkeys-space/l8opensim:latest` | Simulator (main Go binary; SNMP / SSH / HTTPS / flow-export server) | Root `Dockerfile` via `make docker-push` | GitHub Container Registry, on push to `main` and on release tags. |
| `saichler/opensim-web:latest` | L8 web frontend (vnet overlay + HTTPS web proxy on port 9095) | `go/l8/Dockerfile` via `make docker` | Built locally; **not** auto-published. |

If you just want the simulator, use the first image. The second is only
relevant for the optional Layer 8 integration — see
[Architecture → Integration paths](../reference/architecture.md#integration-paths).

## Pull and run the simulator

```bash
docker pull ghcr.io/labmonkeys-space/l8opensim:latest

# The simulator needs TUN + netns privileges. hostNetwork isn't strictly
# required but makes the HTTP control plane reachable on :8080 directly.
docker run --rm -it \
  --cap-add=NET_ADMIN \
  --device=/dev/net/tun \
  --network=host \
  ghcr.io/labmonkeys-space/l8opensim:latest \
  -auto-start-ip 192.168.100.1 -auto-count 10
```

!!! warning "Host FORWARD policy"
    On hosts with Docker installed, the default `FORWARD` chain in iptables
    is `DROP`. The simulator inserts a `FORWARD -i veth-sim-host -j ACCEPT`
    rule at startup so per-device flow exporters can reach external
    collectors. On clean shutdown the rule is removed. See
    [Flow export → Prerequisites](../ops/flow-export.md#prerequisites-for-per-device-source-ip).

## Build locally

```bash
# Simulator image (host platform)
make docker-build

# Simulator image (multi-platform, pushed to the registry)
make docker-push
```

The `docker-push` target pushes `linux/amd64` + `linux/arm64` — override the
tag list with `DOCKER_TAGS="..."`.

## docker-compose

```bash
make docker-up     # docker compose up --build
make docker-down   # docker compose down
```

## Kubernetes

The L8 overlay ships a StatefulSet manifest:

```bash
kubectl apply -f go/l8/opensim.yaml
```

The manifest runs in the `opensim` namespace with `hostNetwork: true` and a
`/data` hostPath volume. For production deployments tune the file-descriptor
and memory limits per the [Scaling](../ops/scaling.md) guide.
