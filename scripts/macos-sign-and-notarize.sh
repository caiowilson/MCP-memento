#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: macos-sign-and-notarize.sh \
  --binary PATH \
  --package PATH \
  --version VERSION \
  --application-identity IDENTITY \
  --installer-identity IDENTITY \
  --keychain PATH \
  --notary-profile PROFILE

Signs a macOS command-line binary, builds a signed flat installer package,
submits the package to Apple's notary service, staples the accepted ticket,
and verifies both signatures plus Gatekeeper acceptance.
EOF
}

binary=""
package_path=""
version=""
application_identity=""
installer_identity=""
keychain=""
notary_profile=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) binary=${2:-}; shift 2 ;;
    --package) package_path=${2:-}; shift 2 ;;
    --version) version=${2:-}; shift 2 ;;
    --application-identity) application_identity=${2:-}; shift 2 ;;
    --installer-identity) installer_identity=${2:-}; shift 2 ;;
    --keychain) keychain=${2:-}; shift 2 ;;
    --notary-profile) notary_profile=${2:-}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

for required in binary package_path version application_identity installer_identity keychain notary_profile; do
  if [[ -z ${!required} ]]; then
    echo "missing required option: ${required//_/-}" >&2
    exit 2
  fi
done

if [[ ! -f $binary ]]; then
  echo "binary not found: $binary" >&2
  exit 1
fi
if [[ ! -f $keychain ]]; then
  echo "keychain not found: $keychain" >&2
  exit 1
fi

package_root=$(mktemp -d "${TMPDIR:-/tmp}/memento-pkg.XXXXXX")
submission_json=$(mktemp "${TMPDIR:-/tmp}/memento-notary.XXXXXX.json")
notary_log=$(mktemp "${TMPDIR:-/tmp}/memento-notary-log.XXXXXX.json")
cleanup() {
  rm -rf "$package_root" "$submission_json" "$notary_log"
}
trap cleanup EXIT

mkdir -p "$(dirname "$package_path")" "$package_root/usr/local/bin"

codesign \
  --force \
  --sign "$application_identity" \
  --options runtime \
  --timestamp \
  --keychain "$keychain" \
  "$binary"
codesign --verify --strict --verbose=2 "$binary"

install -m 0755 "$binary" "$package_root/usr/local/bin/memento-mcp"
pkgbuild \
  --root "$package_root" \
  --identifier com.caiowilson.memento-mcp \
  --version "$version" \
  --install-location / \
  --sign "$installer_identity" \
  --keychain "$keychain" \
  --timestamp \
  "$package_path"

submission_id=""
print_notary_log() {
  if [[ -n $submission_id ]]; then
    xcrun notarytool log "$submission_id" "$notary_log" \
      --keychain-profile "$notary_profile" \
      --keychain "$keychain" || true
    if [[ -s $notary_log ]]; then
      cat "$notary_log" >&2
    fi
  fi
}

if ! xcrun notarytool submit "$package_path" \
  --keychain-profile "$notary_profile" \
  --keychain "$keychain" \
  --wait \
  --timeout 30m \
  --no-progress \
  --output-format json >"$submission_json"; then
  submission_id=$(plutil -extract id raw -o - "$submission_json" 2>/dev/null || true)
  print_notary_log
  exit 1
fi

submission_id=$(plutil -extract id raw -o - "$submission_json" 2>/dev/null || true)
status=$(plutil -extract status raw -o - "$submission_json" 2>/dev/null || true)
if [[ $status != "Accepted" ]]; then
  echo "notarization status was ${status:-unavailable}" >&2
  print_notary_log
  exit 1
fi

cat "$submission_json"
xcrun stapler staple -v "$package_path"
xcrun stapler validate -v "$package_path"
pkgutil --check-signature "$package_path"
spctl --assess --type install --verbose=4 "$package_path"
