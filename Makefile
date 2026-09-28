.PHONY: all fmt generate lint test verify audit build dist package clean

all: build

fmt:
	gofmt -w cmd internal
	pnpm -C frontend run format

generate:
	node scripts/build-frontend.mjs --force

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
