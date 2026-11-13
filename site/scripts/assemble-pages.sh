#!/usr/bin/env bash
# Build the three newest stable tags that contain this site.
# The newest is written at the pages root and under /<tag>/.
# Each older tag is written only under /<tag>/.
# DOCS_BASE, DOCS_VERSION, and DOCS_VERSIONS are the build contract.
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"

versions=()
while IFS= read -r tag; do
	if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		continue
	fi
	if git cat-file -e "${tag}:site/package.json" 2>/dev/null; then
		versions+=("$tag")
	fi
	if [[ ${#versions[@]} -eq 3 ]]; then
		break
	fi
done < <(git tag -l 'v*' --sort=-v:refname)

out="$root/site/dist-pages"
rm -rf "$out"

if [[ ${#versions[@]} -eq 0 ]]; then
	echo "no stable tag contains site/; nothing to publish"
	exit 0
fi

joined=$(IFS=,; echo "${versions[*]}")
latest="${versions[0]}"
mkdir -p "$out"

work=""
cleanup() {
	if [[ -n "$work" && -d "$work" ]]; then
		rm -rf "$work"
	fi
}
trap cleanup EXIT

build_tree() {
	local base="$1"
	local dest="$2"
	local tag="$3"
	rm -rf "$work/site/dist"
	DOCS_BASE="$base" DOCS_VERSION="$tag" DOCS_VERSIONS="$joined" \
		npm run build --prefix "$work/site"
	mkdir -p "$dest"
	cp -a "$work/site/dist/." "$dest/"
}

for tag in "${versions[@]}"; do
	work=$(mktemp -d)
	git archive "$tag" | tar -x -C "$work"
	npm ci --prefix "$work/site"
	if [[ "$tag" == "$latest" ]]; then
		build_tree "/co/" "$out" "$tag"
	fi
	build_tree "/co/${tag}/" "$out/${tag}" "$tag"
	rm -rf "$work"
	work=""
done

test -f "$out/index.html"
echo "published ${joined} with ${latest} at the root"
