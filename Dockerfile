# Build stage runs on the host platform (native, no emulation needed).
# The binary is cross-compiled for the target platform.
FROM --platform=${BUILDPLATFORM} golang:1.26-alpine AS build

ARG TARGETARCH

WORKDIR /src

COPY go/ .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /simulator ./simulator

# ----

FROM alpine:3.21

RUN apk add --no-cache iproute2

COPY --from=build /simulator /usr/local/bin/simulator
COPY go/simulator/resources/ /resources/

EXPOSE 8080/tcp 161/udp

ENTRYPOINT ["/usr/local/bin/simulator"]
