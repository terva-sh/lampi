# terva-sh trees drive Go with a justfile. This Makefile is the same
# checks, for a checkout that does not have just installed. CI inlines
# the commands rather than calling either file. See the justfile.

.PHONY: build test vet fmt ci synthetic-container

build:
	mkdir -p bin
	go build -trimpath -o bin/terva-lampi ./cmd/terva-lampi

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
