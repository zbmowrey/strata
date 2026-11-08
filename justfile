# Strata engine repository tasks (docs/spec/01-architecture.md §11).
set shell := ["bash", "-euo", "pipefail", "-c"]

gobin := `go env GOPATH` + "/bin"
export PATH := gobin + ":" + env_var("PATH")
pkg := "github.com/zbmowrey/strata/engine/internal/version"
version := `git describe --tags --match 'v*' --abbrev=0 2>/dev/null | sed 's/^v//' || true`
commit := `git rev-parse HEAD 2>/dev/null || true`
ldflags := "-s -w -X " + pkg + ".Version=" + (if version == "" { "0.1.0-dev" } else { version }) + " -X " + pkg + ".Commit=" + commit

default:
    @just --list

# Build ./bin/strata.
build:
    cd engine && CGO_ENABLED=0 go build -trimpath -ldflags "{{ldflags}}" -o ../bin/strata ./cmd/strata

# Build and install strata into $(go env GOPATH)/bin (or $PREFIX/bin).
install: build
    #!/usr/bin/env bash
    set -euo pipefail
    dest="${PREFIX:+$PREFIX/bin}"; dest="${dest:-{{gobin}}}"
    mkdir -p "$dest" && install -m 0755 bin/strata "$dest/strata" && echo "$dest/strata"

# Unit, integration and race tests with the per-package coverage floor.
test:
    cd engine && go test -race -shuffle=on -coverprofile=../coverage.out -covermode=atomic ./...
    cd engine && go run ./tools/covercheck -min 80 ../coverage.out

# Every gate, identical to CI.
check: fmt-check lint vuln sast secrets typos shell plugin-lint strata-lint

# Gates over staged changes (git pre-commit hook).
check-staged: fmt-check lint typos shell
    gitleaks protect --staged --no-banner --redact --config .gitleaks.toml

fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out="$(cd engine && gofumpt -l . && goimports -local github.com/zbmowrey/strata -l .)"
    if [ -n "$out" ]; then echo "not formatted (run: just fmt):"; echo "$out"; exit 1; fi

lint:
    cd engine && golangci-lint run --config ../.golangci.yml ./...

vuln:
    cd engine && govulncheck ./...

sast:
    #!/usr/bin/env bash
    set -euo pipefail
    dirs=(); for d in engine plugin scripts; do [ -d "$d" ] && dirs+=("$d"); done
    semgrep scan --quiet --error --metrics=off --config .semgrep/ --config p/golang --config p/typescript --config p/secrets "${dirs[@]}"

secrets:
    gitleaks detect --no-banner --redact --config .gitleaks.toml --source .

typos:
    typos

shell:
    #!/usr/bin/env bash
    set -euo pipefail
    files="$(git ls-files '*.sh' '*.bash')"
    if [ -z "$files" ]; then exit 0; fi
    shellcheck $files && shfmt -d -i 2 $files

plugin-lint:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f plugin/package.json ]; then cd plugin && pnpm install --frozen-lockfile --silent && pnpm run lint; fi

strata-lint:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f .strata/config.toml ]; then just build >/dev/null && ./bin/strata lint; fi

# Apply all formatters.
fmt:
    cd engine && gofumpt -w . && goimports -local github.com/zbmowrey/strata -w .
    if [ -f plugin/package.json ]; then cd plugin && pnpm run fmt; fi

# Performance benchmarks vs. baseline, plus the retrieval bench.
bench:
    cd engine && go test -run '^$' -bench . -benchtime 1x ./... >/dev/null
    just build >/dev/null && ./bin/strata bench perf --budgets "${STRATA_BUDGETS:-strict}"

plugin-check:
    claude plugin validate plugin/
    cd plugin && pnpm install --frozen-lockfile --silent && pnpm run lint
    claude plugin test plugin/

# Run strata doctor against this repository.
doctor: build
    ./bin/strata doctor

# Install the git pre-commit hook (runs `just check-staged`).
hooks:
    printf '#!/bin/sh\nexec just check-staged\n' > .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit && echo .git/hooks/pre-commit

# Tag and publish a release (asks nothing — run only with the owner's go-ahead).
release VERSION:
    @echo "release {{VERSION}}: not yet implemented (Phase 12)" && exit 1
