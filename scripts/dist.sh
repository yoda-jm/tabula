#!/bin/sh
# Builds the release archives into dist/, for every platform, with SHA256SUMS.
#
#   scripts/dist.sh VERSION [NAME]
#
# VERSION is what -version reports. Archives are named tabula-NAME-OS-ARCH
# (tabula-OS-ARCH when NAME is empty, for links that never change). The web
# GUI must be built first (make web).
set -eu
version=$1
name=${2-}
rm -rf build dist
mkdir -p build dist
for target in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64; do
	os=${target%/*} arch=${target#*/}
	dir=tabula-${name:+$name-}$os-$arch
	mkdir -p "build/$dir"
	for cmd in cmd/*/; do
		bin=$(basename "$cmd")
		[ "$os" = windows ] && bin="$bin.exe"
		GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
			-ldflags "-s -w -X main.version=$version" -o "build/$dir/$bin" "./$cmd"
	done
	cp README.md "build/$dir/"
	if [ "$os" = windows ]; then
		(cd build && zip -qr "../dist/$dir.zip" "$dir")
	else
		tar -C build -czf "dist/$dir.tar.gz" "$dir"
	fi
done
(cd dist && sha256sum -- * >SHA256SUMS)
