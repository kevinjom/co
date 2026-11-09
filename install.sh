#!/bin/sh
set -eu

repo="kevinjom/co"

die() {
	printf 'install: %s\n' "$1" >&2
	exit 1
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
	cat <<'EOF'
usage: install.sh [version]

Install co into PREFIX, or /usr/local/bin when PREFIX is unset.
With no version, install the latest release. A prerelease is not
the latest release. A version is a tag with or without the leading v.
EOF
	exit 0
fi

if [ "$#" -gt 1 ]; then
	die "usage: install.sh [version]"
fi

case "$(uname -s)" in
Darwin) goos=darwin ;;
Linux) goos=linux ;;
*) die "unsupported system: $(uname -s)" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) goarch=amd64 ;;
arm64 | aarch64) goarch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v install >/dev/null 2>&1 || die "install is required"

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

if [ "$#" -eq 1 ]; then
	req=$1
elif [ -n "${CO_VERSION:-}" ]; then
	req=$CO_VERSION
else
	req=""
fi

if [ -z "$req" ]; then
	latest="$workdir/latest.json"
	code=$(curl -sS -o "$latest" -w '%{http_code}' \
		-H "Accept: application/vnd.github+json" \
		"https://api.github.com/repos/${repo}/releases/latest" </dev/null) || die "failed to reach GitHub"
	if [ "$code" = "200" ]; then
		tag=$(awk -F '"' '/"tag_name":/ { print $4; exit }' "$latest")
		[ -n "$tag" ] || die "latest release has no tag"
	elif [ "$code" = "404" ]; then
		list="$workdir/releases.json"
		code=$(curl -sS -o "$list" -w '%{http_code}' \
			-H "Accept: application/vnd.github+json" \
			"https://api.github.com/repos/${repo}/releases?per_page=1" </dev/null) || die "failed to reach GitHub"
		[ "$code" = "200" ] || die "GitHub returned ${code} while listing releases"
		pre=$(awk -F '"' '/"tag_name":/ { print $4; exit }' "$list")
		if [ -n "$pre" ]; then
			die "no stable release. The newest prerelease is ${pre}. Pass a version."
		fi
		die "no release"
	else
		die "GitHub returned ${code} for the latest release"
	fi
	case "$tag" in
	v*) ver=${tag#v} ;;
	*) die "release tag ${tag} has no leading v" ;;
	esac
else
	case "$req" in
	v*) ver=${req#v} ;;
	*) ver=$req ;;
	esac
	case "$ver" in
	"" | *[!0-9A-Za-z._-]*) die "invalid version: ${req}" ;;
	esac
	tag="v${ver}"
fi

url="https://github.com/${repo}/releases/download/${tag}/co_${ver}_${goos}_${goarch}"
bin="$workdir/co"
curl -fL -o "$bin" "$url" </dev/null || die "failed to download ${url}"
[ -s "$bin" ] || die "downloaded an empty file from ${url}"

magic=$(od -An -N 4 -t x1 "$bin" | tr -d ' \n')
case "$magic" in
7f454c46 | cffaedfe | cefaedfe | feedface | feedfacf) ;;
*) die "downloaded file is not an executable" ;;
esac
chmod +x "$bin"

prefix="${PREFIX:-/usr/local/bin}"
if [ -e "$prefix" ] && [ ! -d "$prefix" ]; then
	die "${prefix} is not a directory"
fi
if [ ! -d "$prefix" ]; then
	if ! mkdir -p "$prefix" 2>/dev/null; then
		command -v sudo >/dev/null 2>&1 || die "cannot create ${prefix}"
		sudo mkdir -p "$prefix"
	fi
fi
if [ -w "$prefix" ]; then
	install -m 755 "$bin" "${prefix}/co"
else
	command -v sudo >/dev/null 2>&1 || die "${prefix} is not writable"
	sudo install -m 755 "$bin" "${prefix}/co"
fi

printf '%s\n' "installed ${prefix}/co (${tag})"
