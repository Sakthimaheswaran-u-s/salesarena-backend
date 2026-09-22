.PHONY: run build test vet tidy deps up down logs

run:            ## run the API locally (needs Mongo + Redis)
	go run ./cmd/server

build:          ## build ./bin/server
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/server ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

deps:           ## start only Mongo + Redis in Docker
	docker compose up -d mongo redis

up:             ## start Mongo + Redis + API in Docker
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f api
