DSN ?= postgres://postgres:postgres@localhost:5432/odtbank?sslmode=disable

.PHONY: up down migrate migrate-down run test build vet

up:
	docker compose up -d postgres

down:
	docker compose down

migrate:
	migrate -path migrations -database "$(DSN)" up

migrate-down:
	migrate -path migrations -database "$(DSN)" down 1

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

run:
	DATABASE_URL=$(DSN) go run ./cmd/server