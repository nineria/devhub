.PHONY: all web build test check run clean

all: build

# The page is built first because the Go binary embeds web/dist.
web:
	cd web && pnpm install --frozen-lockfile && pnpm run build

build: web
	go build -ldflags='-s -w' -o ostler .

test:
	go vet ./...
	go test -race ./...

check: test
	cd web && pnpm install --frozen-lockfile && pnpm exec tsc --noEmit && pnpm exec vp check src

run: build
	./ostler

clean:
	rm -rf ostler web/dist/assets web/dist/index.html web/dist/favicon.svg
