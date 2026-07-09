# syntax=docker/dockerfile:1.7
# =============================================================================
# Orion 2.0 — production multi-stage image for Google Cloud Run
# =============================================================================
# Builder:  golang:1.25.12-alpine3.24 (pinned digest) — small, matches go.mod 1.25.x
# Runtime:  gcr.io/distroless/static-debian12:nonroot — ~2MB base, CA certs for
#           outbound HTTPS (gamma-api.polymarket.com), no shell/package manager,
#           runs as uid 65532 (nonroot). Prefer over alpine (smaller surface) and
#           scratch (scratch needs manual CA bundle for TLS).
#
# Cloud Run contract:
#   - Listens on 0.0.0.0 via Addr:":"+PORT (app default 8080)
#   - Handles SIGTERM for graceful shutdown (app already does; CR allows ~10s)
#   - gVisor-compatible: no privileged ops, no kernel modules, pure userspace Go
#
# Runtime hardening (deploy-time; not expressible fully in Dockerfile):
#   - Cloud Run: no privileged; secrets via Secret Manager / env (never bake .env)
#   - Local/K8s: --read-only --tmpfs /tmp --cap-drop=ALL --security-opt=no-new-privileges
#   - Keep default seccomp/AppArmor; Cloud Run gen2 uses gVisor
#   - Scan: trivy image <tag>  |  docker scout cves <tag>
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1: build static binary with BuildKit module + compile caches
# -----------------------------------------------------------------------------
FROM golang:1.25.12-alpine3.24@sha256:56961d79ea8129efddcc0b8643fd8a5416b4e6228cfd477e3fd61deb2672c587 AS builder

# CGO off → fully static binary, no glibc/musl runtime deps in final image.
# GOTOOLCHAIN=local → use the image toolchain only (reproducible; no silent downloads).
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local

# Multi-arch builds (buildx sets TARGETOS/TARGETARCH automatically).
ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src

# Dependency graph first: go.mod/go.sum rarely change → layer + cache hit.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Application sources only (.dockerignore excludes secrets, tests, scripts).
COPY . .

# -trimpath: no host paths in the binary
# -ldflags="-s -w": strip symbols/DWARF → smaller image, less reverse-engineering surface
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/orion .

# -----------------------------------------------------------------------------
# Stage 2: minimal non-root runtime
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot@sha256:b7bb25d9f7c31d2bdd1982feb4dafcaf137703c7075dbe2febb41c24212b946f

LABEL org.opencontainers.image.title="orion2.0" \
      org.opencontainers.image.description="Orion API service (Google Cloud Run)" \
      org.opencontainers.image.source="https://github.com/samucap/orion2.0" \
      org.opencontainers.image.licenses="UNLICENSED"

# Cloud Run injects PORT; default matches container contract (8080).
ENV PORT=8080

# Owned by nonroot (65532:65532) so a read-only root FS still works for exec.
COPY --from=builder --chown=nonroot:nonroot /out/orion /orion

# Defense in depth: pin non-root even though :nonroot already defaults to it.
USER nonroot:nonroot

# Document default listen port (actual bind uses $PORT).
EXPOSE 8080

# HEALTHCHECK omitted: distroless has no shell/curl/wget.
# Configure Cloud Run HTTP startup/liveness probe: GET /health → 200 {"status":"ok"}

ENTRYPOINT ["/orion"]
