-include .env
export

run:
	go run cmd/service/main.go

test:
	LOG_LEVEL=PANIC go test ./... --race -v

test-integration:
	LOG_LEVEL=PANIC go test -tags=integration ./internal/integration/... -v

test-sandbox:
	LOG_LEVEL=PANIC go test -tags=sandbox ./internal/payment/mercadopago/ -run Sandbox -count=1 -v

coverage:
	LOG_LEVEL=PANIC go test ./... --coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

up:
	docker compose up --build

down:
	docker compose down -v

migrate-install:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

migrate-up:
	migrate -path migrations -database "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/billing?sslmode=disable" up

migrate-down:
	migrate -path migrations -database "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/billing?sslmode=disable" down

.PHONY: run test test-integration test-sandbox coverage up down migrate-install migrate-up migrate-down
