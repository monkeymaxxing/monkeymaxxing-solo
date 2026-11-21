#!/usr/bin/env bash
# Builds reproducible release archives of set-up-solo-services for every
# supported laptop platform (macOS and Linux, amd64 and arm64) and writes
# SHA256SUMS into the output directory.
#
#   ./build_release_archives.sh 0.1.0 /tmp/release
#
# The version must match solo_version in the template's
# monkeymaxxing_solo_checklist.yml. Run with GNU tar on Linux (or gtar on
# macOS) for deterministic file order and timestamps.
set -euo pipefail
version=${1:?give the version, such as 0.1.0}
output_directory=${2:?give the output directory}
source_directory=$(cd "$(dirname "$0")" && pwd)
template="$source_directory/../skills/monkeymaxxing-solo/systems_repository_template"
solo_version=$(sed -n 's/^solo_version: *"\?\([^"]*\)"\?$/\1/p' "$template/monkeymaxxing_solo_checklist.yml")
[ "$version" = "$solo_version" ] || {
  printf 'build_release_archives.sh: the version is %s, but solo_version in the template'"'"'s monkeymaxxing_solo_checklist.yml is %s; give that version, or change solo_version first. Nothing was built.\n' "$version" "$solo_version" >&2
  exit 1
}

tar_bin=tar
if ! tar --version 2>/dev/null | grep -q 'GNU tar'; then
  if command -v gtar >/dev/null 2>&1; then
    tar_bin=gtar
  else
    printf 'build_release_archives.sh: GNU tar (tar or gtar) is required for reproducible archives.\n' >&2
    exit 1
  fi
fi

sha_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$output_directory"

platforms=(
  "linux amd64 linux-amd64"
  "linux arm64 linux-arm64"
  "darwin amd64 macos-amd64"
  "darwin arm64 macos-arm64"
)

for entry in "${platforms[@]}"; do
  read -r goos goarch label <<< "$entry"
  name="set-up-solo-services-$version-$label"
  mkdir -p "$work/$name"
  (
    cd "$source_directory"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOFLAGS=-mod=readonly \
      go build -trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.version=$version" \
      -o "$work/$name/set-up-solo-services" .
  )
  cp "$source_directory/../LICENSE" "$work/$name/LICENSE"
  chmod 0755 "$work/$name/set-up-solo-services"
  chmod 0644 "$work/$name/LICENSE"
  chmod 0755 "$work/$name"
  "$tar_bin" --sort=name --mtime='2026-01-01 00:00:00Z' --owner=0 --group=0 --numeric-owner \
    -C "$work" -cf - "$name" | gzip -n -9 > "$output_directory/$name.tar.gz"
done

(
  cd "$output_directory"
  sha_cmd -- set-up-solo-services-"$version"-*.tar.gz > SHA256SUMS
  cat SHA256SUMS
)
