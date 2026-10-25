# Tool versions (LIBRARIES §3). Tools run with `go run pkg@version`, so
# nothing is installed globally and every machine uses the same versions.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOVULNCHECK   := golang.org/x/vuln/cmd/govulncheck@v1.9.0
STATICCHECK   := honnef.co/go/tools/cmd/staticcheck@v0.8.2
GITLEAKS      := github.com/zricethezav/gitleaks/v8@v8.30.1

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo.Version=$(VERSION) \
           -X github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo.Commit=$(COMMIT)

BIN := bin
CONSTSQL := $(BIN)/constsql

.PHONY: db-init help build test test-race test-integration lint vet constsql staticcheck gosec vuln gitleaks gitleaks-history check fmt tidy docker up up-web migrate-dev down clean

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

build: ## Build posd and posctl into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/posd ./cmd/posd
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/posctl ./cmd/posctl

test: ## Unit tests (goleak runs in TestMain where goroutines are started)
	go test -count=1 ./...

test-race: ## Unit tests with the race detector (needs a C compiler)
	CGO_ENABLED=1 go test -race -count=1 ./...

test-integration: ## Integration tests on testcontainers Postgres 18 (needs Docker)
	go test -count=1 -tags=integration ./...

fmt: ## Format code
	gofmt -w .

tidy: ## Tidy go.mod
	go mod tidy

vet: ## go vet
	go vet ./...

$(CONSTSQL): $(shell find tools/analyzers -name '*.go' -not -path '*/testdata/*')
	go build -o $(CONSTSQL) ./tools/analyzers/constsql/cmd/constsql

constsql: $(CONSTSQL) ## SQL arguments must be constants (TENANCY §4)
	go vet -tags integration -vettool=$(abspath $(CONSTSQL)) ./...

lint: ## golangci-lint (includes gosec, errcheck, staticcheck, revive, ...)
	go run $(GOLANGCI_LINT) run ./...

staticcheck: ## staticcheck on its own
	go run $(STATICCHECK) ./...

# The standalone gosec v2.29.0 binary cannot read Go 1.27 export data, so
# gosec runs through golangci-lint (built with a current x/tools).
gosec: ## gosec only (through golangci-lint)
	go run $(GOLANGCI_LINT) run --enable-only gosec ./...

vuln: ## govulncheck (fails on reachable vulnerabilities)
	go run $(GOVULNCHECK) ./...

gitleaks: ## Scan commits on this branch that are not on main
	cd .. && go run $(GITLEAKS) git --redact --log-opts="main..HEAD" .

gitleaks-history: ## Scan the whole history (fails until the P0-1 history rewrite runs)
	cd .. && go run $(GITLEAKS) git --redact .

check: vet constsql lint vuln test ## Everything CI runs, except integration tests and gitleaks

docker: ## Build the posd image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t posd:$(VERSION) .

up: ## Start the dev stack (Postgres 18, Caddy, S3 gateway, Mailpit, posd)
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build

COMPOSE_WEB := docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.web.yml --env-file deploy/.env

up-web: ## Dev stack that also serves the tenant app build (frontend/dist)
	$(COMPOSE_WEB) up -d --build

migrate-dev: ## Dev stack: apply platform and tenant migrations
	$(COMPOSE_WEB) run --rm --no-deps --entrypoint /usr/local/bin/posctl app migrate platform up
	$(COMPOSE_WEB) run --rm --no-deps --entrypoint /usr/local/bin/posctl app migrate tenants up

db-init: ## Dev stack: create roles/schemas and apply platform migrations
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env run --rm --no-deps --entrypoint /usr/local/bin/posctl app db bootstrap
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env run --rm --no-deps --entrypoint /usr/local/bin/posctl app migrate platform up

down: ## Stop the dev stack
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env down

clean: ## Remove build output
	rm -rf $(BIN)
