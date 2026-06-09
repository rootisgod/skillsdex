#!/usr/bin/env sh
set -eu

version="${1:-${VERSION:-dev}}"
dist="${DIST_DIR:-dist}"
formula_dir="${FORMULA_DIR:-${dist}/homebrew/Formula}"
homepage="${HOMEBREW_HOMEPAGE:-https://github.com/rootisgod/skillsdex}"
base_url="${RELEASE_BASE_URL:-https://github.com/rootisgod/skillsdex/releases/download}"
tag="${RELEASE_TAG:-v${version}}"

checksums="${dist}/SHA256SUMS"
if [ ! -f "$checksums" ]; then
  echo "Missing ${checksums}; run VERSION=${version} task release first." >&2
  exit 1
fi

sha_for() {
  file="$1"
  awk -v file="$file" '$2 == file { print $1 }' "$checksums"
}

archive_name() {
  os="$1"
  arch="$2"
  printf 'skillsdex-%s-%s-%s.tar.gz' "$version" "$os" "$arch"
}

url_for() {
  file="$1"
  printf '%s/%s/%s' "${base_url%/}" "$tag" "$file"
}

require_sha() {
  file="$1"
  sha="$(sha_for "$file")"
  if [ -z "$sha" ]; then
    echo "Missing checksum for ${file} in ${checksums}" >&2
    exit 1
  fi
  printf '%s' "$sha"
}

darwin_amd64="$(archive_name darwin amd64)"
darwin_arm64="$(archive_name darwin arm64)"
linux_amd64="$(archive_name linux amd64)"
linux_arm64="$(archive_name linux arm64)"

mkdir -p "$formula_dir"
formula="${formula_dir}/skillsdex.rb"
cat >"$formula" <<EOF
class Skillsdex < Formula
  desc "Private hosted skill library and CLI for syncing Codex skills"
  homepage "${homepage}"
  version "${version}"
  license :cannot_represent

  on_macos do
    if Hardware::CPU.arm?
      url "$(url_for "$darwin_arm64")"
      sha256 "$(require_sha "$darwin_arm64")"
    else
      url "$(url_for "$darwin_amd64")"
      sha256 "$(require_sha "$darwin_amd64")"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "$(url_for "$linux_arm64")"
      sha256 "$(require_sha "$linux_arm64")"
    else
      url "$(url_for "$linux_amd64")"
      sha256 "$(require_sha "$linux_amd64")"
    end
  end

  def install
    bin.install "skillsdex"
    bin.install "skillsdexd"
  end

  service do
    run [opt_bin/"skillsdexd", "serve", "--addr", "127.0.0.1:8765", "--db", var/"skillsdex/skillsdex.db"]
    keep_alive true
    log_path var/"log/skillsdex.log"
    error_log_path var/"log/skillsdex.log"
  end

  test do
    assert_match "${version}", shell_output("#{bin}/skillsdex version")
  end
end
EOF

echo "Wrote ${formula}"
