# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.25.0-alpine AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN test "${TARGETOS}/${TARGETARCH}" = "linux/amd64" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-s -w -X github.com/ychiu1211/dsmctl/internal/buildinfo.Version=${VERSION}" \
      -o /out/dsmctl-gateway ./cmd/dsmctl-gateway

FROM scratch

ARG VERSION=dev
LABEL org.opencontainers.image.title="dsmctl gateway" \
      org.opencontainers.image.description="Portable managed MCP gateway for Synology DSM fleets" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.source="https://github.com/ychiu1211/dsmctl"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/dsmctl-gateway /dsmctl-gateway

USER 10001:10001
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/dsmctl-gateway", "healthcheck", "--url", "http://127.0.0.1:8080/healthz"]

ENTRYPOINT ["/dsmctl-gateway"]
CMD ["--listen=0.0.0.0:8080", "--state=/data/gateway.db", "--master-key-file=/run/secrets/master.key", "--bootstrap-file=/run/secrets/bootstrap", "--allowed-hosts=localhost,127.0.0.1", "--allowed-origins=http://localhost:8080,http://127.0.0.1:8080"]
