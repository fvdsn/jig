#!/usr/bin/env bash
# Generate the Homebrew formula for a release.
# Usage: brew-formula.sh <tag> <checksums.txt> > jig.rb
set -euo pipefail

tag="$1"
checksums="$2"
version="${tag#v}"

sha() {
  local sum
  sum=$(awk -v f="jig_${tag}_$1.tar.gz" '$2 == f { print $1 }' "$checksums")
  if [ -z "$sum" ]; then
    echo "missing checksum for jig_${tag}_$1.tar.gz" >&2
    exit 1
  fi
  echo "$sum"
}

url() {
  echo "https://github.com/fvdsn/jig/releases/download/${tag}/jig_${tag}_$1.tar.gz"
}

cat <<EOF
class Jig < Formula
  desc "Manage a workspace of many Git repositories from a single shared schema"
  homepage "https://github.com/fvdsn/jig"
  version "${version}"
  license "MIT"

  on_macos do
    on_arm do
      url "$(url darwin_arm64)"
      sha256 "$(sha darwin_arm64)"
    end
    on_intel do
      url "$(url darwin_amd64)"
      sha256 "$(sha darwin_amd64)"
    end
  end

  on_linux do
    on_arm do
      url "$(url linux_arm64)"
      sha256 "$(sha linux_arm64)"
    end
    on_intel do
      url "$(url linux_amd64)"
      sha256 "$(sha linux_amd64)"
    end
  end

  def install
    bin.install "jig"
  end

  test do
    assert_match "jig v${version}", shell_output("#{bin}/jig --version")
  end
end
EOF
