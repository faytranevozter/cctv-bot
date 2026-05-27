BINARY      := cctv-bot
PKG         := github.com/faytranevozter/cctv-bot
DOCKER_IMG  := cctv-bot:latest
GO          ?= go
LDFLAGS     := -s -w

.PHONY: help run build install tidy fmt vet test clean docker-build docker-run env

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

run: ## Run the bot (loads .env)
	$(GO) run .

build: ## Build static binary into ./bin/$(BINARY)
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/$(BINARY) .

install: ## go install into $GOBIN
	$(GO) install -ldflags="$(LDFLAGS)" .

tidy: ## go mod tidy
	$(GO) mod tidy

fmt: ## go fmt
	$(GO) fmt ./...

vet: ## go vet
	$(GO) vet ./...

test: ## Run tests
	$(GO) test ./... -race -count=1

clean: ## Remove build artifacts
	rm -rf bin
	rm -f $(BINARY)

env: ## Copy .env.example to .env if missing
	@test -f .env || cp .env.example .env && echo ".env ready"

docker-build: ## Build docker image
	docker build -t $(DOCKER_IMG) .

docker-run: ## Run docker image with .env
	docker run --rm --env-file .env --name $(BINARY) $(DOCKER_IMG)
