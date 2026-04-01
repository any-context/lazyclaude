#!/bin/sh
# lazyclaude installer -- downloads the pre-built binary for standalone use.
# For tmux plugin integration, use TPM or clone the repo instead.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/any-context/lazyclaude/prod/install.sh | sh

set -e

REPO="any-context/lazyclaude"
INSTALL_DIR="${LAZYCLAUDE_INSTALL_DIR:-$HOME/.local/bin}"

detect_platform() {
  OS="$(uname -s)"
  ARCH="$(uname -m)"

  case "$OS" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    *)      echo "Unsupported OS: $OS" >&2; exit 1 ;;
  esac

  case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)             echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac

  echo "${OS}_${ARCH}"
}

get_latest_version() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"//;s/".*//'
}

main() {
  PLATFORM="$(detect_platform)"
  VERSION="$(get_latest_version)"

  if [ -z "$VERSION" ]; then
    echo "Error: could not determine latest release version." >&2
    echo "Check https://github.com/${REPO}/releases" >&2
    exit 1
  fi

  TARBALL="lazyclaude_${VERSION#v}_${PLATFORM}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"

  echo "Installing lazyclaude ${VERSION} (${PLATFORM})..."

  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  curl -fsSL "$URL" -o "${TMPDIR}/${TARBALL}"
  tar -xzf "${TMPDIR}/${TARBALL}" -C "$TMPDIR"

  mkdir -p "$INSTALL_DIR"
  install -m 755 "${TMPDIR}/lazyclaude" "${INSTALL_DIR}/lazyclaude"

  echo "Installed to ${INSTALL_DIR}/lazyclaude"

  if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    echo ""
    echo "Add to your PATH if not already:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
  fi
}

main
