BINARY := lazyclaude
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build test test-unit test-e2e test-ssh test-visual test-visual-ssh lint install clean

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

## SSH + real Claude Code E2E (requires Docker Compose + .env with CLAUDE_CODE_OAUTH_TOKEN)
test-ssh-e2e:
	docker compose -f docker-compose.ssh.yml build
	docker compose -f docker-compose.ssh.yml run --rm local \
		"ssh-keyscan -H remote >> /root/.ssh/known_hosts 2>/dev/null && bash tests/integration/scripts/verify_remote_popup.sh lazyclaude"
	docker compose -f docker-compose.ssh.yml down

## Visual E2E tests — capture-pane UI output (requires Docker)
test-visual:
	docker build -f Dockerfile.test -t lazyclaude-test .
	docker run --rm lazyclaude-test bash -c '\
		for f in tests/integration/scripts/verify_*.sh; do \
			echo ""; echo "========== $$(basename $$f) =========="; \
			bash "$$f" lazyclaude || exit 1; \
		done'

## Visual E2E: single script (e.g. make test-visual-popup_stack)
test-visual-%:
	docker build -f Dockerfile.test -t lazyclaude-test .
	docker run --rm lazyclaude-test bash tests/integration/scripts/verify_$*.sh lazyclaude

## Visual E2E: SSH mode (requires Docker Compose)
test-visual-ssh:
	docker compose -f docker-compose.ssh.yml build
	docker compose -f docker-compose.ssh.yml run --rm local \
		"ssh-keyscan -H remote >> /root/.ssh/known_hosts 2>/dev/null && \
		 for f in tests/integration/scripts/verify_*.sh; do \
			echo ''; echo '========== \$$(basename \$$f) =========='; \
			MODE=ssh REMOTE_HOST=remote bash \"\$$f\" lazyclaude || exit 1; \
		 done"
	docker compose -f docker-compose.ssh.yml down

lint:
	golangci-lint run ./...

install: build
	cp bin/$(BINARY) $(GOPATH)/bin/$(BINARY)

clean:
	rm -rf bin/
