include .env
export

export GOOSE_DRIVER=sqlite3
export GOOSE_DBSTRING=./db.db

.PHONY: run
run:
	CGO_ENABLED=1 go mod tidy && go mod download && \
	pplog go run .

.PHONY: deploy
deploy:
	./build/deploy.sh

.PHONY: dry-run
dry-run: goose-reset run-migrate

.PHONY: goose-new
goose-new:
	@read -p "Enter the name of the new migration: " name; \
	goose -dir migrations create $${name// /_} sql

.PHONY: goose-up
goose-up:
	@echo "Running all new database migrations..."
	goose -dir migrations validate
	goose -dir migrations up

.PHONY: goose-down
goose-down:
	@echo "Running all down database migrations..."
	goose -dir migrations down

.PHONY: goose-reset
goose-reset:
	@echo "Dropping everything in database..."
	goose -dir migrations reset

.PHONY: goose-status
goose-status:
	goose -dir migrations status
