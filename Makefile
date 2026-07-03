.PHONY: build run test test-int vet tidy docker

build:
	go build ./...

run:
	go run ./cmd/api

vet:
	go vet ./...

tidy:
	go mod tidy

# Pure unit tests (no database required).
test:
	go test ./...

# Integration + concurrency tests. Point TEST_DATABASE_URL at a throwaway Postgres.
# Example:
#   make test-int TEST_DATABASE_URL="postgres://postgres:test@localhost:55432/puq?sslmode=disable"
#
# -p 1 serializes package execution. All integration packages share ONE database,
# and enrollment's TestMain issues `TRUNCATE ... CASCADE` over the full domain
# table set. Running package binaries in parallel (the default) would let that
# truncate wipe rows other packages have in flight, causing FK-violation
# flakiness. Serializing keeps the shared-DB suite deterministic.
test-int:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./... -count=1 -p 1

docker:
	docker build -t profundiza-uq-api .

# Seed development data. Override DB_CONTAINER / DB / DB_USER if needed.
DB_CONTAINER ?= profundiza-uq-general-postgres-1
DB ?= profundiza_uq
DB_USER ?= postgres

seed:
	docker exec -i $(DB_CONTAINER) psql -U $(DB_USER) -d $(DB) < seed/dev_seed.sql
