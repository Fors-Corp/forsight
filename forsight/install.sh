#!/bin/sh
# Installs the forsight binary for this machine's OS/arch and (on Linux, when
# run as root) sets it up as a systemd service. Usage:
#
#   curl -fsSL https://raw.githubusercontent.com/marcfs31/forsight/main/forsight/install.sh | sh
#
# Honors:
#   FORSIGHT_VERSION      release tag to install, e.g. "forsight-v1.0.0" (default: latest)
#   FORSIGHT_INSTALL_DIR  where to place the binary (default: /usr/local/bin)
#   FORSIGHT_SERVICE_FILE the systemd unit path (default: /etc/systemd/system/forsight.service)
#   FORSIGHT_ENV_FILE     the flags file the unit reads (default: /etc/default/forsight)
# The last two exist for testing this script and for non-standard system
# layouts; nobody running a normal install needs to set them.
set -eu

REPO="marcfs31/forsight"
INSTALL_DIR="${FORSIGHT_INSTALL_DIR:-/usr/local/bin}"
SERVICE_FILE="${FORSIGHT_SERVICE_FILE:-/etc/systemd/system/forsight.service}"
ENV_FILE="${FORSIGHT_ENV_FILE:-/etc/default/forsight}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "forsight: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

version="${FORSIGHT_VERSION:-}"
if [ -z "$version" ]; then
  version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
    | grep -o '"tag_name": *"forsight-v[^"]*"' \
    | head -n1 \
    | sed -E 's/.*"(forsight-v[^"]+)".*/\1/')
  if [ -z "$version" ]; then
    echo "forsight: could not determine the latest release; set FORSIGHT_VERSION explicitly" >&2
    exit 1
  fi
fi

asset="forsight_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${version}/${asset}"
sums_url="https://github.com/${REPO}/releases/download/${version}/sha256sums.txt"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

echo "forsight: downloading ${version} for ${os}/${arch}..."
curl -fsSL "$url" -o "$tmpdir/$asset"

if ! curl -fsSL "$sums_url" -o "$tmpdir/sha256sums.txt"; then
  echo "forsight: could not download sha256sums.txt for ${version} (${sums_url}); refusing to install an unverified binary" >&2
  exit 1
fi

# sha256sum/shasum both print "<hash>  <filename>", optionally with a "*"
# marking binary mode on the filename — strip that before matching.
expected=$(awk -v want="$asset" '{ name = $2; sub(/^\*/, "", name); if (name == want) print $1 }' "$tmpdir/sha256sums.txt")
if [ -z "$expected" ]; then
  echo "forsight: sha256sums.txt has no entry for ${asset}; refusing to install an unverified binary" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmpdir/$asset" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmpdir/$asset" | awk '{print $1}')
else
  echo "forsight: neither sha256sum nor shasum is available to verify the download" >&2
  exit 1
fi

if [ "$actual" != "$expected" ]; then
  echo "forsight: checksum mismatch for ${asset}: expected $expected, got $actual" >&2
  exit 1
fi

tar -xzf "$tmpdir/$asset" -C "$tmpdir"

if [ -w "$INSTALL_DIR" ]; then
  mv "$tmpdir/forsight" "$INSTALL_DIR/forsight"
else
  echo "forsight: $INSTALL_DIR isn't writable, retrying with sudo..."
  sudo mv "$tmpdir/forsight" "$INSTALL_DIR/forsight"
fi
chmod +x "$INSTALL_DIR/forsight"
echo "forsight: installed to $INSTALL_DIR/forsight"
"$INSTALL_DIR/forsight" version

# Optional: register a systemd service on Linux when run as root. Anyone who
# doesn't want this can just run "forsight run" directly instead.
if [ "$os" = "linux" ] && [ "$(id -u)" = "0" ] && command -v systemctl >/dev/null 2>&1; then
  if [ ! -f "$ENV_FILE" ]; then
    cat > "$ENV_FILE" <<'EOF'
# Flags for "forsight run", read by the systemd unit via EnvironmentFile.
# Uncomment and edit as needed, e.g.:
#FORSIGHT_ARGS="--store badger --auth-token <token> --mlaas-url http://localhost:9000"
#
# install.sh never overwrites this file once it exists, so a re-run or
# upgrade keeps whatever you set here.
EOF
    echo "forsight: wrote ${ENV_FILE} (edit it to set --store, --auth-token, --mlaas-url, etc.)"
  fi

  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=forsight observability agent
After=network.target

[Service]
EnvironmentFile=-${ENV_FILE}
ExecStart=${INSTALL_DIR}/forsight run \$FORSIGHT_ARGS
Restart=on-failure
MemoryMax=256M

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now forsight
  echo "forsight: installed and started as a systemd service (systemctl status forsight)"
else
  echo "forsight: run '${INSTALL_DIR}/forsight run' to start it"
fi
