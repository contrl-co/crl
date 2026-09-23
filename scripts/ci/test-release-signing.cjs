// Exercise the configured signing command with the release's pinned cosign.
// Local keys and empty service lists keep this test offline: no OIDC login,
// certificate issuance, or transparency-log upload. This tests the bundle
// format, not public-instance provenance (which is verified after release).
"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const repo = path.resolve(__dirname, "../..");
const cosign = process.env.COSIGN_BIN || "cosign";
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "crl-signing-test-"));
const env = { ...process.env, COSIGN_PASSWORD: "" };

function run(args) {
    return execFileSync(cosign, args, { cwd: temp, env, encoding: "utf8" });
}

try {
    assert.match(run(["version"]), /GitVersion:\s+v3\.1\.2\b/);
    const config = fs.readFileSync(path.join(repo, ".goreleaser.yaml"), "utf8");
    const signing = config.split("\nsigns:\n")[1].split("\nrelease:\n")[0];
    // GoReleaser registers this filename as its signature artifact and
    // includes it in the release upload. No detached .sig/.pem is produced.
    assert.match(signing, /^    signature: "\$\{artifact\}\.sigstore\.json"$/m);
    assert.doesNotMatch(signing, /^    certificate:/m);
    const args = [...signing.matchAll(/^      - (.+)$/gm)].map((match) =>
        match[1].replace(/^"|"$/g, "").replaceAll("${signature}", "checksums.txt.sigstore.json")
            .replaceAll("${artifact}", "checksums.txt"),
    );
    assert.deepEqual(args, ["sign-blob", "--bundle=checksums.txt.sigstore.json", "--yes", "checksums.txt"]);
    const workflow = fs.readFileSync(path.join(repo, ".github/workflows/release.yml"), "utf8");
    assert.match(workflow, /cosign-release: v3\.1\.2\b/);

    fs.writeFileSync(path.join(temp, "checksums.txt"), "local release-signing fixture\n");
    run(["generate-key-pair", "--output-key-prefix", "local"]);
    run(["signing-config", "create", "--out", "signing-config.json"]);
    run(["trusted-root", "create", "--out", "trusted-root.json"]);
    run([...args, "--key", "local.key", "--signing-config", "signing-config.json",
        "--trusted-root", "trusted-root.json"]);

    const bundle = JSON.parse(fs.readFileSync(path.join(temp, "checksums.txt.sigstore.json"), "utf8"));
    assert.equal(bundle.mediaType, "application/vnd.dev.sigstore.bundle.v0.3+json");
    assert.equal(bundle.messageSignature.messageDigest.algorithm, "SHA2_256");
    const payload = fs.readFileSync(path.join(temp, "checksums.txt"));
    assert.equal(bundle.messageSignature.messageDigest.digest,
        crypto.createHash("sha256").update(payload).digest("base64"));
    const publicKey = fs.readFileSync(path.join(temp, "local.pub"));
    const signature = Buffer.from(bundle.messageSignature.signature, "base64");
    assert(crypto.verify("sha256", payload, publicKey, signature));
    assert(!crypto.verify("sha256", Buffer.from("tampered\n"), publicKey, signature));
    signature[0] ^= 1;
    assert(!crypto.verify("sha256", payload, publicKey, signature));
    assert.equal(bundle.verificationMaterial.tlogEntries?.length || 0, 0);
    console.log("pinned cosign release-bundle format and signature checks passed (local keys only)");
} finally {
    fs.rmSync(temp, { recursive: true, force: true });
}
