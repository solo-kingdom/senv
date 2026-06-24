#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-solo-kingdom/senv}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY="senv"

err() {
  echo "error: $*" >&2
  exit 1
}

info() {
  echo "$*"
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *) err "unsupported operating system: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo "amd64" ;;
    aarch64 | arm64) echo "arm64" ;;
    *) err "unsupported architecture: $(uname -m)" ;;
  esac
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

resolve_tag() {
  if [ "$VERSION" = "latest" ]; then
    need_cmd curl
    local api_url="https://api.github.com/repos/${REPO}/releases/latest"
    local tag
    tag="$(curl -fsSL "$api_url" | grep '"tag_name"' | head -n1 | sed 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/')"
    [ -n "$tag" ] || err "failed to resolve latest release for ${REPO}"
    echo "$tag"
  else
    echo "$VERSION"
  fi
}

download_file() {
  local url="$1"
  local dest="$2"
  curl -fsSL "$url" -o "$dest"
}

verify_checksum() {
  local archive="$1"
  local checksums="$2"
  local name
  name="$(basename "$archive")"
  local expected
  expected="$(grep "  ${name}$" "$checksums" | awk '{print $1}')"
  [ -n "$expected" ] || err "checksum not found for ${name}"
  local actual
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  else
    err "required command not found: sha256sum or shasum"
  fi
  [ "$actual" = "$expected" ] || err "checksum mismatch for ${name}"
}

path_contains_dir() {
  local dir="$1"
  case ":$PATH:" in
    *":$dir:"*) return 0 ;;
    *) return 1 ;;
  esac
}

main() {
  need_cmd curl
  need_cmd tar

  local os arch tag asset archive_url checksums_url tmpdir archive checksums
  os="$(detect_os)"
  arch="$(detect_arch)"
  tag="$(resolve_tag)"
  asset="${BINARY}_${tag}_${os}_${arch}.tar.gz"
  archive_url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
  checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"

  tmpdir="$(mktemp -d)"
  trap "rm -rf '$tmpdir'" EXIT
  archive="${tmpdir}/${asset}"
  checksums="${tmpdir}/checksums.txt"

  info "Downloading ${asset}..."
  download_file "$archive_url" "$archive"
  download_file "$checksums_url" "$checksums"
  verify_checksum "$archive" "$checksums"

  if [ ! -d "$INSTALL_DIR" ]; then
    if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
      err "cannot create ${INSTALL_DIR}; try: sudo INSTALL_DIR=${INSTALL_DIR} bash"
    fi
  elif [ ! -w "$INSTALL_DIR" ]; then
    err "no write permission for ${INSTALL_DIR}; try: sudo INSTALL_DIR=${INSTALL_DIR} bash"
  fi

  tar -xzf "$archive" -C "$tmpdir"
  if ! install -m 755 "${tmpdir}/${BINARY}" "${INSTALL_DIR}/${BINARY}" 2>/dev/null; then
    err "failed to install to ${INSTALL_DIR}; try: sudo INSTALL_DIR=${INSTALL_DIR} bash"
  fi

  info "Installed ${BINARY} ${tag} to ${INSTALL_DIR}/${BINARY}"

  if ! path_contains_dir "$INSTALL_DIR"; then
    info ""
    info "Note: ${INSTALL_DIR} is not in your PATH."
    info "Add it with:"
    info "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    info "Or add the line above to your shell profile (~/.bashrc, ~/.zshrc, etc.)."
  fi
}

main "$@"
