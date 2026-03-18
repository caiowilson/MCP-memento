#!/bin/sh
# Install memento-mcp — downloads the latest server binary for your platform.
# Usage: curl -fsSL https://raw.githubusercontent.com/caiowilson/MCP-memento/main/install.sh | sh
set -e

REPO="caiowilson/MCP-memento"
TAG="server/latest"
INSTALL_DIR="${MEMENTO_INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="memento-mcp"

# Detect OS
case "$(uname -s)" in
    Darwin*)  OS="darwin" ;;
    Linux*)   OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
    *)        echo "Unsupported OS: $(uname -s)"; exit 1 ;;
esac

# Detect architecture
case "$(uname -m)" in
    x86_64|amd64) ARCH="x64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)            echo "Unsupported architecture: $(uname -m)"; exit 1 ;;
esac

ASSET="${BINARY_NAME}_${OS}_${ARCH}"
if [ "$OS" = "windows" ]; then
    ASSET="${ASSET}.exe"
    BINARY_NAME="${BINARY_NAME}.exe"
fi

# URL-encode the tag (server/latest -> server%2Flatest)
ENCODED_TAG=$(printf '%s' "$TAG" | sed 's|/|%2F|g')
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${ENCODED_TAG}/${ASSET}"

echo "Installing memento-mcp..."
echo "  OS:      $OS"
echo "  Arch:    $ARCH"
echo "  Asset:   $ASSET"
echo "  From:    $DOWNLOAD_URL"
echo "  To:      $INSTALL_DIR/$BINARY_NAME"
echo ""

# Create install directory
mkdir -p "$INSTALL_DIR"

# Download
TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

HTTP_CODE=$(curl -fsSL -w '%{http_code}' -o "$TMPFILE" "$DOWNLOAD_URL" 2>/dev/null) || true

if [ "$HTTP_CODE" != "200" ] || [ ! -s "$TMPFILE" ]; then
    echo "Error: Failed to download $ASSET (HTTP $HTTP_CODE)"
    echo ""
    echo "This may happen if:"
    echo "  - The release assets haven't been built yet"
    echo "  - The repository is private (authentication required)"
    echo ""
    echo "Alternative: build from source:"
    echo "  go install github.com/${REPO}/cmd/server@latest"
    exit 1
fi

# Install
mv "$TMPFILE" "$INSTALL_DIR/$BINARY_NAME"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

echo "Installed $INSTALL_DIR/$BINARY_NAME"
echo ""

# Check PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        echo "Note: $INSTALL_DIR is not in your PATH."
        echo "Add it with:"
        echo ""
        SHELL_NAME=$(basename "$SHELL" 2>/dev/null || echo "sh")
        case "$SHELL_NAME" in
            zsh)  echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc" ;;
            bash) echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc && source ~/.bashrc" ;;
            fish) echo "  fish_add_path $INSTALL_DIR" ;;
            *)    echo "  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
        esac
        echo ""
        ;;
esac

echo "Next: run 'memento-mcp setup' to configure your IDE."
