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

spec-a:
	go test -v ./crawler -run TestSpec -count=1

spec-b: spec-a

spec-c: spec-a

spec-d: spec-a

spec-e: spec-a

spec-f: spec-a

spec-g: spec-a

spec-h: spec-a

spec-i: spec-a

spec-check: spec-a

.PHONY: build test race run lint cover spec-a spec-b spec-c spec-d spec-e spec-f spec-g spec-h spec-i spec-check
