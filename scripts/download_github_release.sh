#!/usr/bin/env sh
set -eu

repo="${SKILLSDEX_RELEASE_REPO:-rootisgod/skillsdex}"
tag="${RELEASE_TAG:-${VERSION:-latest}}"
install_dir="${INSTALL_DIR:-bin}"
include_daemon="${INCLUDE_DAEMON:-1}"
system_install="${SYSTEM_INSTALL:-0}"
sudo_cmd="${SUDO:-sudo}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    *)
      echo "Unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo "amd64" ;;
    arm64 | aarch64) echo "arm64" ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

verify_checksum() {
  asset="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "${asset}.sha256"
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -c "${asset}.sha256"
    return
  fi
  echo "Missing checksum verifier: install sha256sum or shasum" >&2
  exit 1
}

download_asset() {
  asset="$1"
  installed_name="$2"
  url="${base_url}/${asset}"

  echo "Downloading ${url}"
  if ! curl -fsSL "$url" -o "${tmp}/${asset}"; then
    echo "Release asset not found: ${asset}" >&2
    echo "Repo: ${repo}; release: ${tag}; detected target: ${os}/${arch}" >&2
    echo "If this release predates the target platform, choose another target with SKILLSDEX_RELEASE_OS/SKILLSDEX_RELEASE_ARCH or use a newer release." >&2
    exit 1
  fi
  curl -fsSL "${url}.sha256" -o "${tmp}/${asset}.sha256"
  (cd "$tmp" && verify_checksum "$asset")

  mkdir -p "$install_dir"
  install -m 0755 "${tmp}/${asset}" "${install_dir}/${installed_name}"
  echo "Installed ${install_dir}/${installed_name}"
  if [ "$system_install" = "1" ]; then
    install_system_asset "${tmp}/${asset}" "$installed_name"
  fi
}

install_system_asset() {
  src="$1"
  installed_name="$2"
  dest="/usr/local/bin/${installed_name}"

  if [ "$os" != "linux" ]; then
    echo "SYSTEM_INSTALL=1 is only supported on Linux; skipping ${dest}" >&2
    return
  fi

  if [ -w /usr/local/bin ]; then
    install -m 0755 "$src" "$dest"
  else
    need_cmd "$sudo_cmd"
    "$sudo_cmd" install -m 0755 "$src" "$dest"
  fi
  echo "Installed ${dest}"
}

need_cmd curl
need_cmd install
need_cmd uname

os="${SKILLSDEX_RELEASE_OS:-$(detect_os)}"
arch="${SKILLSDEX_RELEASE_ARCH:-$(detect_arch)}"

case "$tag" in
  latest)
    base_url="https://github.com/${repo}/releases/latest/download"
    ;;
  v*)
    base_url="https://github.com/${repo}/releases/download/${tag}"
    ;;
  *)
    base_url="https://github.com/${repo}/releases/download/v${tag}"
    ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

download_asset "skillsdex-${os}-${arch}" "skillsdex"
if [ "$include_daemon" != "0" ]; then
  download_asset "skillsdexd-${os}-${arch}" "skillsdexd"
fi

echo "Downloaded SkillsDex ${tag} binaries for ${os}/${arch}."
