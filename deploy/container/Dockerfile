# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
FROM --platform=$BUILDPLATFORM golang:1.25.0-alpine@sha256:f18a072054848d87a8077455f0ac8a25886f2397f88bfdd222d6fafbb5bba440 AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=7.3.2-22
ARG REVISION=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN test "${TARGETOS}/${TARGETARCH}" = "linux/amd64" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false \
      -ldflags="-s -w -buildid= -X github.com/derekvery666/dsmctl/internal/buildinfo.Version=${VERSION} -X github.com/derekvery666/dsmctl/internal/buildinfo.Revision=${REVISION}" \
      -o /out/dsmctl-gateway ./cmd/dsmctl-gateway

FROM scratch

ARG VERSION=7.3.2-22
ARG REVISION=unknown
ARG CREATED=1970-01-01T00:00:00Z
LABEL org.opencontainers.image.title="dsmctl gateway" \
      org.opencontainers.image.description="Portable managed MCP gateway for Synology DSM fleets" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.created=$CREATED \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.source="https://github.com/derekvery666/dsmctl"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/dsmctl-gateway /dsmctl-gateway
COPY LICENSE /LICENSE

USER 10001:10001
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/dsmctl-gateway", "healthcheck", "--url", "http://127.0.0.1:8080/healthz"]

ENTRYPOINT ["/dsmctl-gateway"]
CMD ["--listen=0.0.0.0:8080", "--state=/data/gateway.db", "--master-key-file=/run/secrets/master.key", "--allowed-hosts=localhost,127.0.0.1", "--allowed-origins=http://localhost:8080,http://127.0.0.1:8080"]
