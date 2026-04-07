BINARY       := simulator
BUILD_DIR    := go/simulator
GO_DIR       := go
IMAGE        := saichler/opensim-web:latest
SIM_IMAGE    := ghcr.io/labmonkeys-space/l8opensim:latest

# Simulator uses Linux-only syscalls (TUN, network namespaces).
# Cross-compile by default so the binary runs in the container / on Linux hosts.
GOOS   ?= linux
GOARCH ?= amd64

UNAME_S := $(shell uname -s)

.PHONY: all build run test tidy clean docker docker-build docker-push docker-up docker-down help \
        check-go check-docker check-buildx check-linux

all: build

## build: Cross-compile the simulator binary for Linux (GOOS=linux GOARCH=amd64)
build: check-go
	cd $(BUILD_DIR) && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BINARY) .

## tidy: Sync go.mod and go.sum
tidy: check-go
	cd $(GO_DIR) && go mod tidy

## test: Run tests (simulator package requires Linux; other packages run on any OS)
test: check-go
ifneq ($(UNAME_S),Linux)
	@echo "Note: skipping simulator package on $(UNAME_S) — it uses Linux-only syscalls."
	@echo "      Running tests/... only. Use a Linux host or container for full coverage."
	cd $(GO_DIR) && go test ./tests/...
else
	cd $(GO_DIR) && go test ./...
endif

## run: Build and run the simulator (Linux only — requires root for TUN interfaces)
run: check-linux build
	cd $(BUILD_DIR) && sudo ./$(BINARY)

## docker-build: Build the simulator Docker image for the host platform
docker-build: check-docker
	docker build -t $(SIM_IMAGE) .

## docker-push: Build and push a multi-platform image (linux/amd64 + linux/arm64)
docker-push: check-buildx
	docker buildx build \
	  --platform linux/amd64,linux/arm64 \
	  --push \
	  -t $(SIM_IMAGE) .

## docker-up: Start the simulator with docker compose
docker-up: check-docker
	docker compose up --build

## docker-down: Stop and remove the simulator container
docker-down: check-docker
	docker compose down

## docker: Build the L8 web Docker image (linux/amd64)
docker: check-docker
	@echo "Note: requires saichler/builder:latest and saichler/business-security:latest"
	@echo "      to be available in your Docker registry. Pull them first if needed."
	cd go/l8 && docker build --no-cache --platform=linux/amd64 -t $(IMAGE) .

## clean: Remove the simulator binary
clean:
	rm -f $(BUILD_DIR)/$(BINARY)

## help: Show this help
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

# ---------------------------------------------------------------------------
# Dependency guards
# ---------------------------------------------------------------------------

check-go:
	@command -v go >/dev/null 2>&1 || { \
	  echo "Error: 'go' not found."; \
	  echo "       Install Go from https://golang.org/dl/ and ensure it is on your PATH."; \
	  exit 1; \
	}

check-docker:
	@command -v docker >/dev/null 2>&1 || { \
	  echo "Error: 'docker' not found."; \
	  echo "       Install Docker from https://docs.docker.com/get-docker/ and ensure it is on your PATH."; \
	  exit 1; \
	}
	@docker info >/dev/null 2>&1 || { \
	  echo "Error: Docker daemon is not running."; \
	  echo "       Start Docker Desktop (or the Docker service) and retry."; \
	  exit 1; \
	}

check-buildx: check-docker
	@docker buildx version >/dev/null 2>&1 || { \
	  echo "Error: 'docker buildx' not available."; \
	  echo "       Install Docker Desktop >= 2.1 or the buildx plugin."; \
	  exit 1; \
	}
	@# On Linux, multi-platform emulation requires binfmt_misc + QEMU.
	@# On macOS, Docker Desktop and Orbstack provide this natively — no check needed.
	@if [ "$(UNAME_S)" = "Linux" ]; then \
	  docker buildx ls | grep -q 'linux/arm64' || { \
	    echo "Error: active buildx builder does not support linux/arm64."; \
	    echo "       Run: docker run --privileged --rm tonistiigi/binfmt --install all"; \
	    echo "       Then: docker buildx create --use --name multiplatform"; \
	    exit 1; \
	  }; \
	fi

check-linux:
	@[ "$(UNAME_S)" = "Linux" ] || { \
	  echo "Error: 'make run' requires Linux."; \
	  echo "       The simulator uses TUN interfaces and network namespaces"; \
	  echo "       that are not available on $(UNAME_S)."; \
	  echo "       Run it inside a Linux container or VM instead."; \
	  exit 1; \
	}
