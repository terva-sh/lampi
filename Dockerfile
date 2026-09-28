# The lake image: terva-lampi serve on linux/amd64 and linux/arm64.
#
# Build context is the repository root:
#
#	docker buildx build --platform linux/amd64,linux/arm64 \
#		--build-arg VERSION=0.2.0 --build-arg COMMIT=$(git rev-parse --short HEAD) .
#
# `just image` builds the local platform and tags terva-lampi:dev. The
# synthetic test image is e2e/Dockerfile; it is a fixture, not this.
#
# Every RUN below runs on the build platform, and Go cross-compiles, so
# an arm64 image builds on an amd64 host without QEMU.
#
# Both bases are pinned to their multi-arch index. Bump the digest and
# the tag in its comment together.

# golang:1.27-bookworm
FROM --platform=$BUILDPLATFORM docker.io/library/golang@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
# .dockerignore leaves .git out, so go build records no VCS stamp and
# --version reads these instead, as `just build` stamps them.
ARG VERSION=0.0.0
ARG COMMIT=unknown

# CGO_ENABLED=0: the SQLite driver is pure Go and the binary is static.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
	-buildvcs=false \
	-trimpath \
	-ldflags "-s -w -X terva.sh/lampi/internal/cli.version=${VERSION} -X terva.sh/lampi/internal/cli.commit=${COMMIT}" \
	-o /out/terva-lampi ./cmd/terva-lampi

# The final stage has no shell, so the lake directory is made here.
FROM --platform=$BUILDPLATFORM docker.io/library/golang@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS layout
RUN install -d -m 0700 /layout/terva-lampi

# gcr.io/distroless/static-debian12:nonroot: CA certificates, /etc/passwd,
# and user 65532, no shell and no package manager.
FROM gcr.io/distroless/static-debian12@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG VERSION=0.0.0
ARG COMMIT=unknown
ARG CREATED=unknown

LABEL org.opencontainers.image.title="terva-lampi" \
	org.opencontainers.image.description="The terva-lampi session lake server" \
	org.opencontainers.image.source="https://github.com/terva-sh/lampi" \
	org.opencontainers.image.url="https://github.com/terva-sh/lampi" \
	org.opencontainers.image.licenses="MIT" \
	org.opencontainers.image.version="${VERSION}" \
	org.opencontainers.image.revision="${COMMIT}" \
	org.opencontainers.image.created="${CREATED}"

COPY --from=build /out/terva-lampi /usr/local/bin/terva-lampi
# COPY sets owner 0:0 unless told otherwise, and some builders do the
# same for a copy from another stage, so --chown gives the owner. The
# source is the directory's parent: copying a directory copies its
# contents, so terva-lampi arrives as an entry and keeps its 0700 mode,
# and /var/lib, which exists, keeps root. A new named volume mounted on
# it starts with this owner and mode.
COPY --from=layout --chown=65532:65532 /layout/ /var/lib/

# serve and every serve subcommand resolve the lake directory from
# XDG_STATE_HOME, so `docker exec LAKE terva-lampi serve devices` needs
# no --data. The path is the systemd unit's, so a lake moves between
# the two unchanged.
ENV XDG_STATE_HOME=/var/lib

USER 65532:65532
VOLUME ["/var/lib/terva-lampi"]
EXPOSE 8787

# The listener opens after the catalog is migrated, so the start period
# covers an upgrade's migration. Change --addr here too when the command
# moves serve to another port.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5m --retries=3 \
	CMD ["/usr/local/bin/terva-lampi", "serve", "healthcheck", "--addr", "127.0.0.1:8787"]

# Exec form: serve is PID 1 and gets SIGTERM and SIGHUP directly. On
# SIGTERM it drains for up to 50s, past Docker's 10s default, so give
# the container a stop timeout of 60s.
#
# The command needs a device token file. Mount one at the path below,
# or pass another --token-file. TLS terminates in the proxy in front.
ENTRYPOINT ["/usr/local/bin/terva-lampi"]
CMD ["serve", "--addr", "0.0.0.0:8787", "--token-file", "/var/lib/terva-lampi/tokens", "--behind-proxy"]
