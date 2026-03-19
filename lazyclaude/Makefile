BINARY := lazyclaude
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build test test-unit test-e2e test-ssh lint install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/lazyclaude

test:
	go test -race -cover ./...

test-unit:
	go test -race -cover ./internal/...

## Docker E2E tests (requires Docker)
test-e2e:
	docker build -f Dockerfile.test -t lazyclaude-test .
	docker run --rm lazyclaude-test go test -v -timeout 120s ./tests/integration/

## SSH remote tests (requires Docker Compose)
test-ssh:
	docker compose -f docker-compose.ssh.yml build
	docker compose -f docker-compose.ssh.yml run --rm local
	docker compose -f docker-compose.ssh.yml down

lint:
	golangci-lint run ./...

install: build
	cp bin/$(BINARY) $(GOPATH)/bin/$(BINARY)

clean:
	rm -rf bin/
