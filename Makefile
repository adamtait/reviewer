# SPDX-License-Identifier: MIT
#
# The commands CI runs, so that "it passed locally" and "it passed in CI" cannot
# drift apart. `make check` is the whole gate.

SHELL := /bin/sh
# Fail on the first failing stage even when make's output is piped.
.SHELLFLAGS := -eu -c

# Files that carry no SPDX header: prose, configuration, data, and the example
# config a user copies into their own repository.
LICENSE_IGNORE := -ignore '**/*.md' -ignore '**/*.yml' -ignore '**/*.yaml' \
                  -ignore '**/*.json' -ignore '**/*.txt' -ignore 'testdata/**' \
                  -ignore '**/node_modules/**' -ignore '**/dist/**'

.PHONY: check build plugin plugin-test vet fmt-check test staticcheck license-check adrs licenses tools hooks secrets

check: build plugin vet fmt-check test staticcheck license-check adrs licenses

build:
	go build ./...

# The cross-language conformance test drives the real plugin, so the gate builds
# it. A skipped conformance test would hide a drift between the two sides of the
# protocol, which is the one failure neither side's own tests can catch.
plugin:
	@if [ -d plugins/typescript/node_modules ]; then \
		npm --prefix plugins/typescript run build; \
	else \
		echo "plugins/typescript: node_modules missing; run 'npm --prefix plugins/typescript ci'" >&2; \
		exit 1; \
	fi

plugin-test:
	npm --prefix plugins/typescript test

vet:
	go vet ./...

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

test:
	go test ./...

staticcheck:
	staticcheck ./...

license-check:
	addlicense -check -l mit -c "Adam Tait" $(LICENSE_IGNORE) .

adrs:
	go run ./tools/checkadrs

licenses:
	go run ./tools/checklicenses

# Install the developer tools CI pins. Versions here must match .github/workflows/ci.yml.
tools:
	go install honnef.co/go/tools/cmd/staticcheck@2025.1.1
	go install github.com/google/addlicense@v1.1.1

# Opt in to the pre-commit secret scan. Not installed automatically: a hook that
# appears without being asked for is a hook people disable.
hooks:
	git config core.hooksPath .githooks
	@echo "pre-commit secret scan enabled. Disable with: git config --unset core.hooksPath"

# Scan this repository's entire history, which is what CI does.
secrets:
	gitleaks detect --source . --config .gitleaks.toml --log-opts=--all --redact
