DSN ?= postgres://postgres:postgres@localhost:5432/odtbank?sslmode=disable

.PHONY: up down migrate migrate-down run admin test build vet web-dev web-test

up:
	docker compose up -d postgres minio

down:
	docker compose down

migrate:
	docker compose run --rm migrate up

migrate-down:
	docker compose run --rm migrate down 1

build:
	go build ./cmd/... ./internal/...

vet:
	go vet ./cmd/... ./internal/...

test:
	go test ./cmd/... ./internal/...

run:
	DATABASE_URL=$(DSN) MINIO_ENDPOINT=localhost:9000 go run ./cmd/server

admin:
	DATABASE_URL=$(DSN) go run ./cmd/admin

web-dev:
	cd web && npm run dev

web-test:
	cd web && npm run build
