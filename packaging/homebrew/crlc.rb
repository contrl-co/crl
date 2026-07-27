# Homebrew formula for crlc. Releases regenerate this file with real
# version/URL/sha256 values via GoReleaser's brews publisher (see
# .goreleaser.yaml); this checked-in copy documents the shape and lets
# the formula be reviewed like any other source file.
class Crlc < Formula
  desc "CRL (CONTRL Rule Language) toolchain: lint, compile, fmt, eval, graph"
  homepage "https://gitlab.com/contrl-group/crl"
  version "0.0.0-dev"
  license "AGPL-3.0-only"

  on_macos do
    on_arm do
      url "https://gitlab.com/contrl-group/crl/-/releases/#{version}/downloads/crlc_darwin_arm64.tar.gz"
      sha256 "REPLACED_AT_RELEASE"
    end
    on_intel do
      url "https://gitlab.com/contrl-group/crl/-/releases/#{version}/downloads/crlc_darwin_amd64.tar.gz"
      sha256 "REPLACED_AT_RELEASE"
    end
  end

  on_linux do
    on_arm do
      url "https://gitlab.com/contrl-group/crl/-/releases/#{version}/downloads/crlc_linux_arm64.tar.gz"
      sha256 "REPLACED_AT_RELEASE"
    end
    on_intel do
      url "https://gitlab.com/contrl-group/crl/-/releases/#{version}/downloads/crlc_linux_amd64.tar.gz"
      sha256 "REPLACED_AT_RELEASE"
    end
  end

  def install
    bin.install "crlc"
  end

  test do
    assert_match "editions: v1", shell_output("#{bin}/crlc version")
    (testpath/"gate.crl").write <<~CRL
      crl v1
      package brewtest.demo
      bundle brewtest.gate
      rule gate
      \ttarget brewtest.gate
      \tcollector src org api from /x.json
      \t\tsignal ready bool from x.ready ttl 30d
      \tneed ready == true
      \tquorum src
    CRL
    assert_match "ok", shell_output("#{bin}/crlc lint #{testpath}/gate.crl")
  end
end
