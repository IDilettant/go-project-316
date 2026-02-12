test:
	go test -v ./...

race:
	go test -v ./... -race

lint:
	golangci-lint run ./...

build:
	go build -o bin/hexlet-go-crawler ./cmd/hexlet-go-crawler

run:
	go run ./cmd/hexlet-go-crawler "$(URL)"

cover:
	go test -v ./... -race -count=1 -tags=integration \
		-covermode=atomic -coverpkg=./... -coverprofile=coverage.out
	go tool cover -func=coverage.out


.PHONY: build test race run lint cover
