#!/bin/sh
# Verified standalone installer for memento-mcp.
# curl -fsSL https://github.com/caiowilson/MCP-memento/releases/download/server%2Flatest/install.sh | sh -s -- --clients codex,claude
set -eu

REPO="${MEMENTO_REPO:-caiowilson/MCP-memento}"
INSTALL_DIR="${MEMENTO_INSTALL_DIR:-$HOME/.local/bin}"
CLIENTS="auto"
SETUP=1
VERSION=""

usage() {
    cat <<'EOF'
Usage: install.sh [options]

  --clients LIST       Comma-separated clients: codex, claude, claude-code, auto, none
  --install-dir DIR    Destination directory (default: $MEMENTO_INSTALL_DIR or ~/.local/bin)
  --version VERSION    Install a specific server version instead of server/latest
  --no-setup           Install and verify the binary without configuring clients
  -h, --help           Show this help
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --clients)
            [ "$#" -ge 2 ] || { echo "Error: --clients requires a value" >&2; exit 2; }
            CLIENTS=$2
            shift 2
            ;;
        --clients=*) CLIENTS=${1#*=}; shift ;;
        --install-dir)
            [ "$#" -ge 2 ] || { echo "Error: --install-dir requires a value" >&2; exit 2; }
            INSTALL_DIR=$2
            shift 2
            ;;
        --install-dir=*) INSTALL_DIR=${1#*=}; shift ;;
        --version)
            [ "$#" -ge 2 ] || { echo "Error: --version requires a value" >&2; exit 2; }
            VERSION=${2#v}
            shift 2
            ;;
        --version=*) VERSION=${1#*=}; VERSION=${VERSION#v}; shift ;;
        --no-setup) SETUP=0; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Error: unknown option $1" >&2; usage >&2; exit 2 ;;
    esac
done

[ -n "$INSTALL_DIR" ] || { echo "Error: install directory is empty" >&2; exit 2; }
command -v curl >/dev/null 2>&1 || { echo "Error: curl is required" >&2; exit 1; }

case "$(uname -s)" in
    Darwin*) OS="darwin" ;;
    Linux*) OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
    *) echo "Error: unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    x86_64|amd64) ARCH="x64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Error: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

BINARY_NAME="memento-mcp"
ASSET="memento-mcp_${OS}_${ARCH}"
if [ "$OS" = "windows" ]; then
    BINARY_NAME="memento-mcp.exe"
    ASSET="${ASSET}.exe"
fi

if [ -n "$VERSION" ]; then
    if ! printf '%s\n' "$VERSION" | awk -F. 'NF == 3 && $1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ { ok=1 } END { exit !ok }'; then
        echo "Error: --version must be a semantic version such as 0.11.0" >&2
        exit 2
    fi
    TAG="server/v${VERSION}"
else
    TAG="server/latest"
fi
ENCODED_TAG=$(printf '%s' "$TAG" | sed 's|/|%2F|g')
BASE_URL="${MEMENTO_RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download/${ENCODED_TAG}}"
BINARY_URL="${BASE_URL}/${ASSET}"
CHECKSUM_URL="${BINARY_URL}.sha256"

normalize_clients() {
    value=$1
    case "$value" in
        none|"") NORMALIZED_CLIENTS=""; return ;;
        auto)
            detected=""
            if command -v codex >/dev/null 2>&1; then detected="codex"; fi
            if command -v claude >/dev/null 2>&1; then
                if claude plugin list --json 2>/dev/null | grep -q 'memento@memento-mcp'; then
                    CLAUDE_PLUGIN_ACTIVE=1
                elif [ -n "$detected" ]; then
                    detected="${detected},claude"
                else
                    detected="claude"
                fi
            fi
            NORMALIZED_CLIENTS=$detected
            return
            ;;
    esac
    normalized=""
    old_ifs=$IFS
    IFS=,
    for client in $value; do
        IFS=$old_ifs
        client=$(printf '%s' "$client" | tr '[:upper:]' '[:lower:]' | tr -d ' ')
        case "$client" in
            codex) slug="codex" ;;
            claude|claude-code) slug="claude" ;;
            *) echo "Error: unsupported client: $client" >&2; return 1 ;;
        esac
        if [ -n "$normalized" ]; then normalized="${normalized},${slug}"; else normalized=$slug; fi
        IFS=,
    done
    IFS=$old_ifs
    NORMALIZED_CLIENTS=$normalized
}

SELECTED_CLIENTS=""
NORMALIZED_CLIENTS=""
CLAUDE_PLUGIN_ACTIVE=0
if [ "$SETUP" -eq 1 ]; then
    normalize_clients "$CLIENTS" || exit 2
    SELECTED_CLIENTS=$NORMALIZED_CLIENTS
fi

mkdir -p "$INSTALL_DIR"
TARGET="${INSTALL_DIR}/${BINARY_NAME}"
PREVIOUS="${TARGET}.previous"
if [ -L "$TARGET" ]; then
    echo "Error: refusing to replace symlinked target $TARGET" >&2
    exit 1
fi

STAGED=$(mktemp "${INSTALL_DIR}/.memento-install.XXXXXX")
CHECKSUM=$(mktemp "${INSTALL_DIR}/.memento-checksum.XXXXXX")
BACKUP=""
cleanup() {
    if [ -n "$STAGED" ]; then rm -f "$STAGED"; fi
    if [ -n "$CHECKSUM" ]; then rm -f "$CHECKSUM"; fi
    if [ -n "$BACKUP" ]; then rm -f "$BACKUP"; fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

echo "Downloading ${ASSET} from ${TAG}..."
curl -fsSL "$BINARY_URL" -o "$STAGED"
curl -fsSL "$CHECKSUM_URL" -o "$CHECKSUM"
[ -s "$STAGED" ] || { echo "Error: downloaded binary is empty" >&2; exit 1; }
BINARY_BYTES=$(wc -c < "$STAGED" | tr -d ' ')
CHECKSUM_BYTES=$(wc -c < "$CHECKSUM" | tr -d ' ')
[ "$BINARY_BYTES" -le 268435456 ] || { echo "Error: downloaded binary exceeds 256 MiB" >&2; exit 1; }
[ "$CHECKSUM_BYTES" -le 4096 ] || { echo "Error: checksum sidecar is too large" >&2; exit 1; }

EXPECTED=$(awk -v asset="$ASSET" '
    NR != 1 || NF != 2 { bad=1; next }
    {
        name=$2
        sub(/^\*/, "", name)
        if (length($1) != 64 || $1 ~ /[^0-9A-Fa-f]/ || name != asset) bad=1
        hash=tolower($1)
    }
    END { if (NR != 1 || bad) exit 1; print hash }
' "$CHECKSUM") || { echo "Error: invalid checksum sidecar for ${ASSET}" >&2; exit 1; }

if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "$STAGED" | awk '{print tolower($1)}')
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL=$(shasum -a 256 "$STAGED" | awk '{print tolower($1)}')
elif command -v openssl >/dev/null 2>&1; then
    ACTUAL=$(openssl dgst -sha256 "$STAGED" | awk '{print tolower($NF)}')
else
    echo "Error: sha256sum, shasum, or openssl is required" >&2
    exit 1
fi
[ "$ACTUAL" = "$EXPECTED" ] || { echo "Error: checksum mismatch for ${ASSET}" >&2; exit 1; }

chmod 0755 "$STAGED"
CANDIDATE_VERSION=$("$STAGED" version 2>/dev/null) || { echo "Error: downloaded binary failed its version preflight" >&2; exit 1; }
if ! printf '%s\n' "$CANDIDATE_VERSION" | awk -F. 'NF == 3 && $1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ { ok=1 } END { exit !ok }'; then
    echo "Error: downloaded binary reported invalid version: $CANDIDATE_VERSION" >&2
    exit 1
fi
if [ -n "$VERSION" ] && [ "$CANDIDATE_VERSION" != "$VERSION" ]; then
    echo "Error: downloaded binary reported $CANDIDATE_VERSION, expected $VERSION" >&2
    exit 1
fi

if [ -e "$TARGET" ]; then
    BACKUP=$(mktemp "${INSTALL_DIR}/.memento-backup.XXXXXX")
    cp -p "$TARGET" "$BACKUP"
fi
if ! mv -f "$STAGED" "$TARGET"; then
    echo "Error: could not replace $TARGET; close running clients and retry" >&2
    exit 1
fi
STAGED=""

INSTALLED_VERSION=$("$TARGET" version 2>/dev/null) || {
    if [ -n "$BACKUP" ]; then mv -f "$BACKUP" "$TARGET"; BACKUP=""; else rm -f "$TARGET"; fi
    echo "Error: installed binary failed validation; previous binary restored" >&2
    exit 1
}
if [ "$INSTALLED_VERSION" != "$CANDIDATE_VERSION" ]; then
    if [ -n "$BACKUP" ]; then mv -f "$BACKUP" "$TARGET"; BACKUP=""; else rm -f "$TARGET"; fi
    echo "Error: installed binary changed during replacement; previous binary restored" >&2
    exit 1
fi
if [ -n "$BACKUP" ]; then
    mv -f "$BACKUP" "$PREVIOUS"
    BACKUP=""
fi

echo "Installed memento-mcp ${INSTALLED_VERSION} at ${TARGET}"

if [ "$SETUP" -eq 1 ]; then
    if [ -n "$SELECTED_CLIENTS" ]; then
        "$TARGET" setup --clients="$SELECTED_CLIENTS" --force
        "$TARGET" doctor --clients="$SELECTED_CLIENTS"
    elif [ "$CLAUDE_PLUGIN_ACTIVE" -eq 1 ]; then
        echo "Claude Code already uses the Memento plugin; no duplicate standalone registration was added."
    else
        echo "No Codex or Claude CLI detected; run '$TARGET setup --clients=codex,claude' later."
    fi
fi

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "Note: add $INSTALL_DIR to PATH to run memento-mcp directly." ;;
esac
