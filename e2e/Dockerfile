# Local image for synthetic container smoke.
#
# Build context is the repository root:
#
#	docker build -f e2e/Dockerfile -t terva-lampi:synthetic .
#
# The image contains the terva-lampi binary and empty mount points.
# It does not contain config, tokens, homes, or session fixtures.
# `make synthetic-image` tags it terva-lampi:synthetic and does not push.

FROM golang:1.27-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 0.0.0 and unknown are the defaults. Pass --build-arg VERSION and
# COMMIT to stamp a build. make synthetic-image does not.
ARG VERSION=0.0.0
ARG COMMIT=unknown

# -buildvcs=false because the commit is an ldflag, and a copied .git
# (or a worktree .git file) makes `go build` fail before it compiles.
# CGO_ENABLED=0 keeps the binary static for the distroless stage.
RUN CGO_ENABLED=0 go build \
	-buildvcs=false \
	-trimpath \
	-ldflags "-s -w -X terva.sh/lampi/internal/cli.version=${VERSION} -X terva.sh/lampi/internal/cli.commit=${COMMIT}" \
	-o /out/terva-lampi ./cmd/terva-lampi

# Distroless has no shell, so the empty mount points are created here.
FROM build AS layout
RUN mkdir -p \
	/empty/lake \
	/empty/state \
	/empty/config \
	/empty/homes/terva \
	/empty/homes/claude \
	/empty/homes/codex \
	/empty/homes/opencode

# static includes CA certificates. serve on loopback does not dial out;
# the certs are there for a later client command in the same image.
FROM gcr.io/distroless/static-debian12

COPY --from=build /out/terva-lampi /usr/local/bin/terva-lampi
COPY --from=layout /empty/ /

# The smoke driver fills these. /state and /config are the XDG dirs
# named in e2e/README.md. Harness homes stay unset so the driver can
# point TERVA_HOME and the other roots at /homes/* itself.
ENV XDG_STATE_HOME=/state \
	XDG_CONFIG_HOME=/config

ENTRYPOINT ["/usr/local/bin/terva-lampi"]
CMD ["serve", "--data", "/lake", "--addr", "127.0.0.1:8787"]
