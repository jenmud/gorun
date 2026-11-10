#!/bin/sh
# Makes the files recipe 7 uploads, in build/ next to this script:
#   build/release.tar.gz   SIZE_MB megabytes of random data (default 1024, so 1 GB)
#   build/release.sha256   its checksum, for the upload's verify
#   build/site/            a static site: 200 files of 64 KB to 1 MB in a few folders
set -eu
cd "$(dirname "$0")"
mkdir -p build/site/css build/site/img build/site/js
dd if=/dev/urandom of=build/release.tar.gz bs=1048576 count="${SIZE_MB:-1024}" 2>/dev/null
(cd build && sha256sum release.tar.gz > release.sha256 2>/dev/null || shasum -a 256 release.tar.gz > release.sha256)
i=0
while [ $i -lt 200 ]; do
	case $((i % 3)) in 0) dir=css ;; 1) dir=img ;; *) dir=js ;; esac
	dd if=/dev/urandom of="build/site/$dir/file-$i" bs=65536 count=$((1 + i % 16)) 2>/dev/null
	i=$((i + 1))
done
du -sh build/release.tar.gz build/site
