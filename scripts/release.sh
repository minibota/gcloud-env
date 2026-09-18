#!/usr/bin/env bash
set -euo pipefail
version=${1:?Usage: scripts/release.sh vX.Y.Z}
if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Expected a release version such as v0.1.0" >&2
  exit 1
fi
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
out="$root/releases/$version"
mkdir -p "$out"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  stage=$(mktemp -d)
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags="-s -w -X main.version=$version" -o "$stage/gcloud-env" ./cmd/gcloud-env
  cp LICENSE README.md "$stage/"
  tar -czf "$out/gcloud-env_${version}_${os}_${arch}.tar.gz" -C "$stage" gcloud-env LICENSE README.md
  rm -rf "$stage"
done
(cd "$out" && sha256sum ./*.tar.gz > checksums.txt)
echo "Release archives: $out"
