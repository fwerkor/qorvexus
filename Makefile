APP := qorvexus

.PHONY: fmt test race build run docker-build ci

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test:
	go test ./...

race:
	go test -race ./...

build:
	go build -trimpath -o bin/$(APP) ./cmd/qorvexus

run:
	go run ./cmd/qorvexus start

docker-build:
	docker build -t $(APP):local .

ci:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"
	go test ./...
	go build -trimpath ./...
