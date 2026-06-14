#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'USAGE'
Usage: scripts/update-brew-tap.sh [--version vX.Y.Z] [--tap /path/to/homebrew-tap] [--repo owner/name] [--formula name] [--commit] [--push]

Updates the Homebrew tap formula for kent with a new tag tarball + sha256.

Defaults:
  --version : $KENT_VERSION, $GITHUB_REF_NAME, or latest git tag in this repo
  --repo    : respawn-llc/kent
  --formula : kent
  --tap     : $KENT_TAP_PATH, $HOMEBREW_TAP_PATH, else ../homebrew-tap (relative to repo root)

Flags:
  --commit  : commit the formula update in the tap repo
  --push    : push the commit (implies --commit)
USAGE
}

require_option_value() {
	local flag="$1"
	local value="${2:-}"
	if [[ -n "$value" && "$value" != --* ]]; then
		return
	fi
	echo "${flag} requires a value" >&2
	usage >&2
	exit 1
}

version=""
repo="respawn-llc/kent"
formula="kent"
tap_dir=""
do_commit="false"
do_push="false"

unset_git_local_env() {
	local config_count="${GIT_CONFIG_COUNT:-}"
	unset \
		GIT_ALTERNATE_OBJECT_DIRECTORIES \
		GIT_COMMON_DIR \
		GIT_CONFIG \
		GIT_CONFIG_COUNT \
		GIT_CONFIG_PARAMETERS \
		GIT_DIR \
		GIT_GLOB_PATHSPECS \
		GIT_GRAFT_FILE \
		GIT_ICASE_PATHSPECS \
		GIT_IMPLICIT_WORK_TREE \
		GIT_INDEX_FILE \
		GIT_INTERNAL_SUPER_PREFIX \
		GIT_LITERAL_PATHSPECS \
		GIT_NAMESPACE \
		GIT_NOGLOB_PATHSPECS \
		GIT_NO_REPLACE_OBJECTS \
		GIT_OBJECT_DIRECTORY \
		GIT_PREFIX \
		GIT_REPLACE_REF_BASE \
		GIT_SHALLOW_FILE \
		GIT_WORK_TREE

	if [[ -n "$config_count" && "$config_count" =~ ^[0-9]+$ ]]; then
		local i
		for ((i = 0; i < config_count; i++)); do
			unset "GIT_CONFIG_KEY_${i}" "GIT_CONFIG_VALUE_${i}"
		done
	fi
}

unset_git_local_env

while [[ $# -gt 0 ]]; do
	case "$1" in
	--version)
		require_option_value "--version" "${2:-}"
		version="$2"
		shift 2
		;;
	--repo)
		require_option_value "--repo" "${2:-}"
		repo="$2"
		shift 2
		;;
	--formula)
		require_option_value "--formula" "${2:-}"
		formula="$2"
		shift 2
		;;
	--tap)
		require_option_value "--tap" "${2:-}"
		tap_dir="$2"
		shift 2
		;;
	--commit)
		do_commit="true"
		shift
		;;
	--push)
		do_commit="true"
		do_push="true"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "Unknown arg: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if ! repo_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
	echo "Not inside a git repo" >&2
	exit 1
fi

if [[ -z "$version" ]]; then
	if [[ -n "${KENT_VERSION:-}" ]]; then
		version="${KENT_VERSION}"
	elif [[ -n "${GITHUB_REF_NAME:-}" ]]; then
		version="${GITHUB_REF_NAME}"
	elif [[ -n "${GITHUB_REF:-}" ]]; then
		version="${GITHUB_REF##*/}"
	else
		version="$(git -C "$repo_root" describe --tags --abbrev=0)"
	fi
fi

if [[ -z "$tap_dir" ]]; then
	if [[ -n "${KENT_TAP_PATH:-}" ]]; then
		tap_dir="$KENT_TAP_PATH"
	elif [[ -n "${HOMEBREW_TAP_PATH:-}" ]]; then
		tap_dir="$HOMEBREW_TAP_PATH"
	elif [[ -d "$repo_root/../homebrew-tap" ]]; then
		tap_dir="$repo_root/../homebrew-tap"
	else
		echo "Tap repo not found. Provide --tap or set HOMEBREW_TAP_PATH" >&2
		exit 1
	fi
fi

formula_path="$tap_dir/Formula/${formula}.rb"
formula_class="$(printf '%s\n' "$formula" | awk -F'[-_]' '{for (i = 1; i <= NF; i++) if ($i != "") printf toupper(substr($i, 1, 1)) substr($i, 2)}')"
url="https://github.com/${repo}/archive/refs/tags/${version}.tar.gz"

tmp_file="$(mktemp)"
tmp_formula="$(mktemp)"
cleanup() {
	rm -f "$tmp_file" "$tmp_formula"
}
trap cleanup EXIT

curl -fsSL "$url" -o "$tmp_file"
if command -v sha256sum >/dev/null 2>&1; then
	sha256="$(sha256sum "$tmp_file" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
	sha256="$(shasum -a 256 "$tmp_file" | awk '{print $1}')"
else
	echo "sha256sum or shasum required" >&2
	exit 1
fi

mkdir -p "$(dirname "$formula_path")"
cat >"$tmp_formula" <<EOF
class ${formula_class} < Formula
  desc "Minimal terminal coding agent for professional engineering workflows"
  homepage "https://github.com/respawn-llc/kent"
  url "$url"
  sha256 "$sha256"
  license "AGPL-3.0-only"

  bottle do
    root_url "https://ghcr.io/v2/respawn-llc/tap"
  end

  depends_on "go" => :build
  depends_on "node" => :build
  depends_on "pnpm" => :build
  depends_on "ripgrep"

  def install
    ENV["KENT_VERSION"] = version.to_s
    system "bash", "scripts/build.sh", "--output", bin/"kent"
  end

  def post_install
    output = Utils.safe_popen_read(bin/"kent", "service", "restart", "--if-installed").strip
    ohai output unless output.empty?
  rescue => e
    opoo "Kent background service restart failed after update: #{e.message}"
  end

  def caveats
    <<~EOS
      Homebrew does not install the Kent server background service.

      If you want one shared background server for all Kent frontends (~70 MB RAM), run:
        kent service install
    EOS
  end

  test do
    assert_match "Usage of kent:", shell_output("#{bin}/kent --help 2>&1")
  end
end
EOF

if [[ ! -f "$formula_path" ]] || ! cmp -s "$tmp_formula" "$formula_path"; then
	mv "$tmp_formula" "$formula_path"
	tmp_formula="$(mktemp)"
fi

chmod 0644 "$formula_path"

if [[ "$do_commit" == "true" ]]; then
	git -C "$tap_dir" add "$formula_path"
	if git -C "$tap_dir" diff --cached --quiet; then
		echo "No formula changes to commit"
	else
		git -C "$tap_dir" commit -m "${formula} ${version}"
	fi
fi

if [[ "$do_push" == "true" ]]; then
	git -C "$tap_dir" push
fi

echo "Updated ${formula_path}"
echo "  url: $url"
echo "  sha256: $sha256"
