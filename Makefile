APP := qorvexus

.PHONY: fmt generate test race build run docker-build ci

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

generate:
	go generate ./internal/socialpluginautoload

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
	tmp=$$(mktemp); \
	cp internal/socialpluginautoload/imports_gen.go "$$tmp"; \
	go generate ./internal/socialpluginautoload; \
	diff -u "$$tmp" internal/socialpluginautoload/imports_gen.go; \
	rm -f "$$tmp"
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"
	go test ./...
	go build -trimpath ./...
