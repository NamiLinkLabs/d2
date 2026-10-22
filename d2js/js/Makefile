.POSIX:
.PHONY: all
all: fmt build test

.PHONY: fmt
fmt: node_modules
	if [ "$${D2JS_SKIP_FMT-}" = 1 ]; then echo "fmt: skipped (D2JS_SKIP_FMT=1)"; else prefix "$@" ../../ci/sub/bin/fmt.sh && prefix "$@" rm -f yarn.lock; fi

.PHONY: build
build: fmt
	prefix "$@" ./ci/build.sh

.PHONY: dev
dev: build
	prefix "$@" bun run dev

.PHONY: test
test: build
	prefix "$@" bun test:all

.PHONY: node_modules
node_modules:
	prefix "$@" bun install $${CI:+--frozen-lockfile}
