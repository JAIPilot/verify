#!/bin/sh
set -eu

repository=JAIPilot/verify
command -v curl >/dev/null 2>&1 || { echo 'Install curl first.' >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo 'Install tar first.' >&2; exit 1; }

case "$(uname -s)" in
  Darwin) platform=darwin ;;
  Linux) platform=linux ;;
  *) echo 'This installer supports macOS and Linux. See the README for Windows.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) architecture=amd64 ;;
  arm64|aarch64) architecture=arm64 ;;
  *) echo "Unsupported CPU architecture: $(uname -m)" >&2; exit 1 ;;
esac

version="${VERIFY_VERSION:-}"
if [ -z "$version" ]; then
  latest=$(curl -fsSL --connect-timeout 10 --max-time 30 -o /dev/null -w '%{url_effective}' \
    "https://github.com/$repository/releases/latest")
  version="${latest##*/}"
fi
case "$version" in v*) ;; *) version="v$version" ;; esac
printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || {
  echo 'Could not determine a stable Verify release.' >&2; exit 1;
}

asset="verify_${version}_${platform}_${architecture}.tar.gz"
base="https://github.com/$repository/releases/download/$version"
install_dir="${VERIFY_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$install_dir"
temporary=$(mktemp -d "$install_dir/.verify-install.XXXXXX")
trap 'exit_status=$?; rm -rf "$temporary"; exit "$exit_status"' 0
trap 'exit 1' HUP INT TERM

echo "Installing Verify $version for $platform/$architecture..." >&2
curl -fsSL --retry 2 --connect-timeout 10 --max-time 180 \
  "$base/$asset" -o "$temporary/$asset"
curl -fsSL --retry 2 --connect-timeout 10 --max-time 30 \
  "$base/checksums.txt" -o "$temporary/checksums.txt"

expected=$(awk -v name="$asset" '$2 == name { print $1 }' "$temporary/checksums.txt")
printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' || {
  echo 'Release checksum is missing or invalid. Existing installation unchanged.' >&2; exit 1;
}
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$temporary/$asset" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$temporary/$asset" | awk '{ print $1 }')
else
  echo 'Install sha256sum or shasum to verify the download.' >&2; exit 1
fi
[ "$actual" = "$expected" ] || {
  echo 'Checksum mismatch. Existing installation unchanged.' >&2; exit 1;
}

tar -xzf "$temporary/$asset" -C "$temporary" verify
chmod 755 "$temporary/verify"
[ "$("$temporary/verify" --version)" = "${version#v}" ] || {
  echo 'Downloaded executable failed its version check. Existing installation unchanged.' >&2; exit 1;
}
mv -f "$temporary/verify" "$install_dir/verify"
echo "Installed $install_dir/verify" >&2
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'Add this directory to your PATH:\n  export PATH="%s:$PATH"\n' "$install_dir" >&2 ;;
esac
echo 'Run: verify --help (install and sign in to Codex or Claude Code first)' >&2
