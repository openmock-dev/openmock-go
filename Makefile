# openmock-go — common developer tasks.
# `make` (or `make help`) lists the targets.

BIN     := bin/openmock
EXAMPLE ?= examples/01-minimal.yml

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the openmock binary into ./bin
	go build -o $(BIN) ./cmd/openmock

.PHONY: install
install: ## Install openmock into $GOBIN / $GOPATH/bin
	go install ./cmd/openmock

.PHONY: test
test: ## Run all tests with the race detector
	go test -race ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format the code in place
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-clean (what CI runs)
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

.PHONY: check
check: fmt-check vet test ## Run the full CI check locally (fmt-check + vet + test)

.PHONY: run
run: build ## Serve an example (override with EXAMPLE=examples/05-polling.yml)
	$(BIN) serve $(EXAMPLE)

.PHONY: demo
demo: build ## Run a scripted, self-contained curl walkthrough
	./examples/demo.sh

.PHONY: clean
clean: ## Remove build output
	rm -rf bin
