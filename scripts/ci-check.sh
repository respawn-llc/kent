#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$repo_root"

read_version() {
	local version="${BUILDER_VERSION:-}"
	if [ -z "$version" ] && [ -f VERSION ]; then
		version="$(tr -d ' \n' <VERSION)"
	fi
	printf '%s' "${version#v}"
}

run_format() {
	echo "==> verify formatting"
	local unformatted
	unformatted="$(gofmt -l .)"
	if [ -n "$unformatted" ]; then
		echo "The following files are not gofmt-formatted:"
		echo "$unformatted"
		exit 1
	fi
}

run_frontend_deps_policy() {
	if [ ! -f apps/scripts/check-dependency-policy.mjs ]; then
		return
	fi
	echo "==> frontend dependency policy"
	if ! command -v node >/dev/null 2>&1; then
		echo "node is required to check frontend dependency policy" >&2
		exit 2
	fi
	node apps/scripts/check-dependency-policy.mjs
}

run_vet() {
	echo "==> go vet"
	go vet ./...
}

run_build() {
	echo "==> go build"
	local version
	version="$(read_version)"
	if [ -n "$version" ]; then
		BUILDER_VERSION="$version" bash scripts/build.sh --output ./bin/builder
		return
	fi
	bash scripts/build.sh --output ./bin/builder
}

run_test() {
	echo "==> test"
	./scripts/test.sh
}

mode="${1:-all}"

case "$mode" in
all)
	run_frontend_deps_policy
	run_format
	run_vet
	run_build
	run_test
	;;
deps)
	run_frontend_deps_policy
	;;
format)
	run_format
	;;
vet)
	run_vet
	;;
build)
	run_build
	;;
test)
	run_test
	;;
*)
	echo "Unknown mode: $mode" >&2
	echo "Usage: $0 [all|deps|format|vet|build|test]" >&2
	exit 1
	;;
esac
