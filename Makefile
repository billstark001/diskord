.PHONY: all fmt generate lint test verify audit build dist package clean

all: build

fmt:
	go run github.com/a-h/templ/cmd/templ fmt internal/ui
	gofmt -w cmd internal tools
	go run github.com/a-h/templ/cmd/templ generate -path internal/ui

generate:
	go run ./tools/prepare.go
	go run github.com/a-h/templ/cmd/templ generate -path internal/ui

lint:
	./scripts/lint.sh

test:
	go test ./...

verify:
	./scripts/verify.sh

audit:
	go run golang.org/x/vuln/cmd/govulncheck ./...

build:
	./scripts/build.sh

dist:
	./scripts/dist.sh

clean:
	rm -rf .build dist
