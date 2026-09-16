#!/usr/bin/env bats
# Tests for install.sh: checksum verification and the systemd unit/env-file
# handling. Run with `make test-install` (bats-core; brew install bats-core
# or apt-get install bats). Not part of the Go test suite — install.sh has
# no Go runtime to test against.
#
# install.sh talks to two real hosts (api.github.com and
# github.com/.../releases/download/...) and, on Linux as root, writes real
# system files. Both are faked here: a "curl" shim on PATH serves fixture
# files by request basename instead of hitting the network, and
# FORSIGHT_SERVICE_FILE/FORSIGHT_ENV_FILE (install.sh's own test hooks)
# redirect the systemd-unit path under the test's own tmpdir. "uname" and
# "id" are shimmed only in the tests that need the Linux-root branch; every
# other test runs against the real ones, so it genuinely exercises whatever
# OS bats itself is running on.

setup() {
  WORK="$(mktemp -d)"
  FAKEBIN="$WORK/fakebin"
  FIXTURES="$WORK/fixtures"
  mkdir -p "$FAKEBIN" "$FIXTURES"

  INSTALL_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/install.sh"

  FORSIGHT_VERSION="forsight-v9.9.9"
  FORSIGHT_INSTALL_DIR="$WORK/opt/bin"
  FORSIGHT_SERVICE_FILE="$WORK/etc/systemd/system/forsight.service"
  FORSIGHT_ENV_FILE="$WORK/etc/default/forsight"
  mkdir -p "$FORSIGHT_INSTALL_DIR" "$(dirname "$FORSIGHT_SERVICE_FILE")" "$(dirname "$FORSIGHT_ENV_FILE")"
  export FORSIGHT_VERSION FORSIGHT_INSTALL_DIR FORSIGHT_SERVICE_FILE FORSIGHT_ENV_FILE

  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
  esac
  ASSET="forsight_${os}_${arch}.tar.gz"

  # A real gzip tarball whose extracted "forsight" is a script standing in
  # for the real binary — install.sh runs "$INSTALL_DIR/forsight version"
  # right after extracting it, so it has to actually be executable.
  tarball_root="$WORK/tarball_root"
  mkdir -p "$tarball_root"
  cat > "$tarball_root/forsight" <<'EOF'
#!/bin/sh
echo "forsight test-fixture version"
EOF
  chmod +x "$tarball_root/forsight"
  tar -czf "$FIXTURES/$ASSET" -C "$tarball_root" forsight

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$FIXTURES/$ASSET" | awk '{print $1}' > "$WORK/asset.sha256"
  else
    shasum -a 256 "$FIXTURES/$ASSET" | awk '{print $1}' > "$WORK/asset.sha256"
  fi

  cat > "$FAKEBIN/curl" <<'SHIM'
#!/bin/sh
# Fake curl: resolves a request by the URL's basename against
# $FAKE_CURL_FIXTURES instead of the network. A "<name>.fail" marker next to
# a fixture makes that request fail, the way a 404 would.
url=""
outfile=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) outfile="$2"; shift 2 ;;
    http://*|https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
name=$(basename "$url")
if [ -f "${FAKE_CURL_FIXTURES}/${name}.fail" ]; then
  echo "fake curl: simulated failure for $url" >&2
  exit 22
fi
if [ -f "${FAKE_CURL_FIXTURES}/${name}" ]; then
  cp "${FAKE_CURL_FIXTURES}/${name}" "$outfile"
  exit 0
fi
echo "fake curl: no fixture for $url" >&2
exit 22
SHIM
  chmod +x "$FAKEBIN/curl"
  export FAKE_CURL_FIXTURES="$FIXTURES"

  PATH="$FAKEBIN:$PATH"
  export PATH
}

teardown() {
  rm -rf "$WORK"
}

# Writes dist/sha256sums.txt-shaped content into the fixtures dir, the same
# format `make release` produces (Makefile's release target).
write_sums() {
  printf '%s  %s\n' "$1" "$ASSET" > "$FIXTURES/sha256sums.txt"
}

@test "installs a release whose checksum matches" {
  write_sums "$(cat "$WORK/asset.sha256")"

  run "$INSTALL_SH"

  [ "$status" -eq 0 ]
  [ -x "$FORSIGHT_INSTALL_DIR/forsight" ]
  [[ "$output" == *"installed to $FORSIGHT_INSTALL_DIR/forsight"* ]]
  [[ "$output" == *"forsight test-fixture version"* ]]
}

@test "refuses to install when the checksum doesn't match" {
  write_sums "0000000000000000000000000000000000000000000000000000000000000000000000000000"

  run "$INSTALL_SH"

  [ "$status" -ne 0 ]
  [[ "$output" == *"checksum mismatch"* ]]
  [ ! -e "$FORSIGHT_INSTALL_DIR/forsight" ]
}

@test "refuses to install when sha256sums.txt has no entry for the asset" {
  printf '%s  %s\n' "deadbeef" "forsight_someother_arch.tar.gz" > "$FIXTURES/sha256sums.txt"

  run "$INSTALL_SH"

  [ "$status" -ne 0 ]
  [[ "$output" == *"no entry for ${ASSET}"* ]]
  [ ! -e "$FORSIGHT_INSTALL_DIR/forsight" ]
}

@test "refuses to install when sha256sums.txt can't be downloaded" {
  # No sha256sums.txt fixture at all: the fake curl 404s on it, same as a
  # real release that predates this feature or a workflow that forgot to
  # upload it.
  run "$INSTALL_SH"

  [ "$status" -ne 0 ]
  [[ "$output" == *"could not download sha256sums.txt"* ]]
  [ ! -e "$FORSIGHT_INSTALL_DIR/forsight" ]
}

@test "off Linux (or not root), the binary installs and no systemd files appear" {
  write_sums "$(cat "$WORK/asset.sha256")"

  run "$INSTALL_SH"

  [ "$status" -eq 0 ]
  [[ "$output" == *"run '$FORSIGHT_INSTALL_DIR/forsight run' to start it"* ]]
  [ ! -e "$FORSIGHT_SERVICE_FILE" ]
  [ ! -e "$FORSIGHT_ENV_FILE" ]
}

# The remaining tests exercise the Linux-as-root branch by shimming uname,
# id and systemctl regardless of what bats itself is running on.
linux_root_shims() {
  cat > "$FAKEBIN/uname" <<'SHIM'
#!/bin/sh
case "$1" in
  -s) echo "Linux" ;;
  -m) echo "x86_64" ;;
  *) echo "Linux" ;;
esac
SHIM
  cat > "$FAKEBIN/id" <<'SHIM'
#!/bin/sh
echo "0"
SHIM
  cat > "$FAKEBIN/systemctl" <<SHIM
#!/bin/sh
echo "\$*" >> "$WORK/systemctl.calls"
exit 0
SHIM
  chmod +x "$FAKEBIN/uname" "$FAKEBIN/id" "$FAKEBIN/systemctl"

  # The tarball built in setup() is named for bats' real host arch; rebuild
  # it (and its checksum) for the arch/os the shimmed uname now reports.
  ASSET="forsight_linux_amd64.tar.gz"
  tar -czf "$FIXTURES/$ASSET" -C "$tarball_root" forsight
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$FIXTURES/$ASSET" | awk '{print $1}' > "$WORK/asset.sha256"
  else
    shasum -a 256 "$FIXTURES/$ASSET" | awk '{print $1}' > "$WORK/asset.sha256"
  fi
  write_sums "$(cat "$WORK/asset.sha256")"
}

@test "as Linux root, writes the env file once with a commented FORSIGHT_ARGS default" {
  linux_root_shims

  run "$INSTALL_SH"

  [ "$status" -eq 0 ]
  [ -f "$FORSIGHT_ENV_FILE" ]
  grep -q '#FORSIGHT_ARGS=' "$FORSIGHT_ENV_FILE"
}

@test "as Linux root, never overwrites an existing env file" {
  linux_root_shims
  echo 'FORSIGHT_ARGS="--store badger --auth-token s3cr3t"' > "$FORSIGHT_ENV_FILE"

  run "$INSTALL_SH"

  [ "$status" -eq 0 ]
  run cat "$FORSIGHT_ENV_FILE"
  [ "$output" = 'FORSIGHT_ARGS="--store badger --auth-token s3cr3t"' ]
}

@test "as Linux root, the unit reads the env file and forwards FORSIGHT_ARGS to ExecStart" {
  linux_root_shims

  run "$INSTALL_SH"

  [ "$status" -eq 0 ]
  [ -f "$FORSIGHT_SERVICE_FILE" ]
  grep -q "EnvironmentFile=-${FORSIGHT_ENV_FILE}" "$FORSIGHT_SERVICE_FILE"
  grep -q "ExecStart=${FORSIGHT_INSTALL_DIR}/forsight run \$FORSIGHT_ARGS" "$FORSIGHT_SERVICE_FILE"
}

@test "as Linux root, enables and starts the service" {
  linux_root_shims

  run "$INSTALL_SH"

  [ "$status" -eq 0 ]
  [ -f "$WORK/systemctl.calls" ]
  grep -q "daemon-reload" "$WORK/systemctl.calls"
  grep -q "enable --now forsight" "$WORK/systemctl.calls"
}
