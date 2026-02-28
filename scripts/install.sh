#!/usr/bin/env bash
set -euo pipefail

# pgmanager installer
# Usage: curl -sSL https://raw.githubusercontent.com/jfoltran/pgmanager/main/scripts/install.sh | bash

REPO="jfoltran/pgmanager"
INSTALL_DIR="/usr/local/bin"
BINARY="pgmanager"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)   echo "linux" ;;
        Darwin*)  echo "darwin" ;;
        *)        error "Unsupported OS: $(uname -s)" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              error "Unsupported architecture: $(uname -m)" ;;
    esac
}

# Get latest release version from GitHub
get_latest_version() {
    local url="https://api.github.com/repos/${REPO}/releases/latest"
    if command -v curl &>/dev/null; then
        curl -sSL "$url" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'
    elif command -v wget &>/dev/null; then
        wget -qO- "$url" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'
    else
        error "Neither curl nor wget found. Please install one of them."
    fi
}

# Download file
download() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -sSL -o "$dest" "$url"
    elif command -v wget &>/dev/null; then
        wget -qO "$dest" "$url"
    fi
}

main() {
    info "Installing pgmanager..."

    local os arch version
    os=$(detect_os)
    arch=$(detect_arch)

    info "Detected platform: ${os}/${arch}"

    version=${PGMANAGER_VERSION:-$(get_latest_version)}
    if [ -z "$version" ]; then
        error "Could not determine latest version. Set PGMANAGER_VERSION manually."
    fi
    info "Version: ${version}"

    # Download binary
    local asset="${BINARY}_${version#v}_${os}_${arch}.tar.gz"
    local url="https://github.com/${REPO}/releases/download/${version}/${asset}"
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    info "Downloading ${url}..."
    download "$url" "${tmp_dir}/${asset}"

    # Download checksum
    local checksum_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"
    download "$checksum_url" "${tmp_dir}/checksums.txt" 2>/dev/null || true

    # Verify checksum if available
    if [ -f "${tmp_dir}/checksums.txt" ]; then
        local expected
        expected=$(grep "${asset}" "${tmp_dir}/checksums.txt" | awk '{print $1}')
        if [ -n "$expected" ]; then
            local actual
            if command -v sha256sum &>/dev/null; then
                actual=$(sha256sum "${tmp_dir}/${asset}" | awk '{print $1}')
            elif command -v shasum &>/dev/null; then
                actual=$(shasum -a 256 "${tmp_dir}/${asset}" | awk '{print $1}')
            fi
            if [ -n "$actual" ] && [ "$expected" != "$actual" ]; then
                error "Checksum mismatch! Expected: ${expected}, Got: ${actual}"
            fi
            info "Checksum verified."
        fi
    else
        warn "Checksums not available, skipping verification."
    fi

    # Extract
    info "Extracting..."
    tar -xzf "${tmp_dir}/${asset}" -C "${tmp_dir}"

    # Install
    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmp_dir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    else
        info "Requesting sudo to install to ${INSTALL_DIR}..."
        sudo mv "${tmp_dir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    fi
    chmod +x "${INSTALL_DIR}/${BINARY}"

    info "pgmanager installed to ${INSTALL_DIR}/${BINARY}"
    "${INSTALL_DIR}/${BINARY}" --help 2>/dev/null | head -3 || true
    echo ""
    info "Installation complete!"
}

main "$@"
