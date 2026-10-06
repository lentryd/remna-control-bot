.PHONY: test build run web

test:
	go vet ./... && go test ./...

web:
	cd web && bun install && bun run build

build: web
	CGO_ENABLED=0 go build -o bin/bot ./cmd/bot

run:
	set -a && . ./.env && set +a && go run ./cmd/bot
