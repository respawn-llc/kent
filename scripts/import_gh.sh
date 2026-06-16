#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat <<'USAGE'
Usage: ./scripts/import_gh.sh <number-or-url>

Imports one GitHub issue into the Kent Task workflow linked as the default for
the current working directory.
USAGE
}

fail() {
	echo "$1" >&2
	exit 1
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		fail "$1 is required."
	fi
}

parse_issue_ref() {
	local ref="$1"
	repo=""
	number=""

	if [[ "$ref" =~ ^[0-9]+$ ]]; then
		repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
		number="$ref"
		return
	fi

	local normalized="${ref#http://}"
	normalized="${normalized#https://}"
	normalized="${normalized#www.}"
	normalized="${normalized%%\?*}"
	normalized="${normalized%%#*}"

	case "$normalized" in
	github.com/*/*/issues/*)
		local path="${normalized#github.com/}"
		local owner="${path%%/*}"
		local rest="${path#*/}"
		local repo_name="${rest%%/*}"
		local issue_path="${rest#*/issues/}"
		number="${issue_path%%/*}"
		repo="$owner/$repo_name"
		;;
	*)
		fail "Expected a GitHub issue number or URL."
		;;
	esac

	if [[ ! "$number" =~ ^[0-9]+$ ]]; then
		fail "Expected a GitHub issue number or URL."
	fi
}

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
kent_bin="${KENT_BIN:-}"
if [ -z "$kent_bin" ]; then
	if [ -x "$repo_root/bin/kent" ]; then
		kent_bin="$repo_root/bin/kent"
	else
		kent_bin="kent"
	fi
fi

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
	usage
	exit 0
fi

if [ "$#" -ne 1 ]; then
	usage
	exit 2
fi

require_command gh
require_command jq
if ! command -v "$kent_bin" >/dev/null 2>&1 && [ ! -x "$kent_bin" ]; then
	fail "kent is required. Set KENT_BIN or build ./bin/kent."
fi

repo=""
number=""
parse_issue_ref "$1"

tmpdir="$(mktemp -d)"
cleanup() {
	if command -v trash >/dev/null 2>&1; then
		trash "$tmpdir" >/dev/null 2>&1 || true
	else
		find "$tmpdir" -depth -delete >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

issue_file="$tmpdir/issue.json"
comments_file="$tmpdir/comments.json"
body_file="$tmpdir/body.md"

gh api "repos/$repo/issues/$number" >"$issue_file"

if [ "$(jq -r 'has("pull_request")' "$issue_file")" = "true" ]; then
	fail "GH #$number in $repo is a pull request, not an issue."
fi

title="$(jq -r '.title // ""' "$issue_file")"
issue_body="$(jq -r '.body // ""' "$issue_file")"
issue_url="$(jq -r '.html_url // ""' "$issue_file")"
import_date="$(date +%F)"

{
	if [ -n "$issue_body" ]; then
		printf '%s\n\n' "$issue_body"
	fi
	printf 'imported from GH #%s on %s\n' "$number" "$import_date"
} >"$body_file"

task_title="GH #$number: $title"
create_json="$("$kent_bin" task create --project . --title "$task_title" --body-file "$body_file" --source-url "$issue_url" --json)"

task_ref="$(jq -r '.summary.short_id // ""' <<<"$create_json")"
task_id="$(jq -r '.summary.task_id // .summary.id // ""' <<<"$create_json")"
if [ -z "$task_ref" ] || [ "$task_ref" = "null" ]; then
	fail "Imported task was created, but its short id could not be read from kent output."
fi
echo "Created Kent task $task_ref ($task_id) for GH #$number."

gh api --paginate --slurp "repos/$repo/issues/$number/comments" >"$comments_file"

comment_count="$(jq '[.[][]] | length' "$comments_file")"
if [ "$comment_count" -eq 0 ]; then
	echo "Imported GH #$number into $task_ref with no comments."
	exit 0
fi

comment_index=0
while IFS= read -r comment_json; do
	comment_index=$((comment_index + 1))
	comment_author="$(jq -r '.user.login // "unknown"' <<<"$comment_json")"
	comment_body="$(jq -r '.body // ""' <<<"$comment_json")"
	comment_file="$tmpdir/comment-$comment_index.md"
	printf '%s\n' "$comment_body" >"$comment_file"
	"$kent_bin" task comment add --project . "$task_ref" --author user --author-id "$comment_author" --body-file "$comment_file"
done < <(jq -c '.[][]' "$comments_file")

echo "Imported GH #$number into $task_ref with $comment_count comments."
