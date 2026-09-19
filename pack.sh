#!/usr/bin/env bash
set -euo pipefail

NAME="go-mosaic"
TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "linux/386"
  "linux/arm"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
  "windows/386"
  "freebsd/amd64"
)

rm -rf pack pack.zip
mkdir -p pack

echo "==> Building ${NAME} release binaries..."

for target in "${TARGETS[@]}"; do
  os="${target%%/*}"
  arch="${target##*/}"
  output_name="${NAME}_${os}_${arch}"

  echo "--> Building ${os}/${arch}..."

  binary_name="${NAME}"
  if [ "${os}" = "windows" ]; then
    binary_name="${NAME}.exe"
  fi

  work_dir=$(mktemp -d)
  CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" go build -ldflags="-s -w" -o "${work_dir}/${binary_name}" .

  zip_file="${output_name}.zip"
  (
    cd "${work_dir}"
    zip -q "${zip_file}" "${binary_name}"
  )

  mv "${work_dir}/${zip_file}" pack/
  rm -rf "${work_dir}"

  echo "    Packaged pack/${zip_file}"
done

echo "==> Generating SHA256 checksums..."
(
  cd pack
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum *.zip > checksums.txt
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 *.zip > checksums.txt
  fi
)

zip -q -r pack.zip pack/
echo "==> All done! Artifacts created in pack/ and pack.zip"
