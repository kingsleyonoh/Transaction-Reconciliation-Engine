.PHONY: test vet lint build dev run migrate-up migrate-down clean

# Variables
BINARY_NAME=recon
MAIN_PATH=./cmd/recon
DATABASE_URL?=postgres://recon:recon_dev@localhost:5432/reconciliation?sslmode=disable
MIGRATE_PATH=./migrations

# Build
build:
	go build -o $(BINARY_NAME) $(MAIN_PATH)

# Run the server
run: build
	./$(BINARY_NAME) serve

# Development with hot reload (requires air: go install github.com/air-verse/air@latest)
dev:
	air -c .air.toml || go run $(MAIN_PATH) serve

# Testing
test:
	go test ./... -count=1 -v

test-race:
	go test ./... -count=1 -v -race

test-cover:
	go test ./... -count=1 -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Linting
vet:
	go vet ./...

lint: vet

# Database migrations (requires golang-migrate CLI)
migrate-up:
	migrate -path $(MIGRATE_PATH) -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path $(MIGRATE_PATH) -database "$(DATABASE_URL)" down 1

migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir $(MIGRATE_PATH) -seq $$name

# Docker
docker-up:
	docker compose up -d

docker-down:
	docker compose down

# Clean
clean:
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
