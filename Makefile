# terva-sh trees drive Go with a justfile. This Makefile is the same
# checks, for a checkout that does not have just installed. CI inlines
# the commands rather than calling either file. See the justfile.

.PHONY: build test vet fmt ci synthetic-container dev dev-serve dev-clean

# The same stamp as the justfile. 0.0.0 means nothing was stamped.
# -buildvcs=false only inside a linked git worktree; the justfile says why.
VERSION ?= 0.0.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILDVCS := $(shell if [ "$$(git rev-parse --git-dir 2>/dev/null)" != "$$(git rev-parse --git-common-dir 2>/dev/null)" ]; then echo "-buildvcs=false"; fi)
LDFLAGS := -s -w -X terva.sh/lampi/internal/cli.version=$(VERSION) -X terva.sh/lampi/internal/cli.commit=$(COMMIT)

build:
	mkdir -p bin
	go build $(BUILDVCS) -trimpath -ldflags "$(LDFLAGS)" -o bin/terva-lampi ./cmd/terva-lampi
	@echo "built bin/terva-lampi ($(VERSION), $(COMMIT))"

# internal/accept is the MVP acceptance gate (architecture section 7).
test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

ci: vet test build
	@diff=$$(gofmt -l .); \
	if [ -n "$$diff" ]; then \
		echo "gofmt issues in:"; \
		echo "$$diff"; \
		exit 1; \
	fi

# Local image for synthetic container smoke. Tags terva-lampi:synthetic
# and stops. It does not run tests and does not push. CI does not
# call this.
synthetic-container:
	docker build -f e2e/Dockerfile -t terva-lampi:synthetic .

# Development runs apart from a live lake and agent. The justfile's dev
# recipes say why. ARGS carries the command: `make dev ARGS=status`.
DEV_DIR := $(CURDIR)/.dev
LAMPI_DEV_ADDR ?= 127.0.0.1:18787
DEV_ENV := XDG_CONFIG_HOME=$(DEV_DIR)/config XDG_STATE_HOME=$(DEV_DIR)/state LAMPI_SERVER=http://$(LAMPI_DEV_ADDR) LAMPI_TOKEN_FILE=

dev-serve: build
	env $(DEV_ENV) bin/terva-lampi serve --data $(DEV_DIR)/lake --addr $(LAMPI_DEV_ADDR) $(ARGS)

dev: build
	env $(DEV_ENV) bin/terva-lampi $(ARGS)

dev-clean:
	rm -rf $(DEV_DIR)
