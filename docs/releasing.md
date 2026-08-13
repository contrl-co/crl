# Releasing

Releases are tag-driven and fully automated; a human decides *when*,
CI decides *whether*.

## Entry criteria

A release tag may be cut only when, on `main`:

1. the full pipeline is green — including the golden-corpus
   determinism gate on every platform with an attached runner;
2. the changelog has an entry for the version;
3. if any canonical output changed: a new edition has been declared in
   [spec/editions.md](../spec/editions.md) and the golden corpus was
   regenerated in the same PR that changed it. **A determinism change
   without an edition is a release blocker with no override path.**

## Cutting a release

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

`vX.Y.Z` is the only tag spelling for new releases. It is simultaneously the
GitHub release tag and the version consumed by Go modules; GoReleaser strips
the leading `v` when it renders binary and Homebrew version strings. The
migrated `0.1.0-beta04`, `0.1.0-beta05`, and `0.1.0` tags remain immutable
historical releases, and the existing `v0.1.0` alias remains the Go module tag
for the same `0.1.0` commit.

The GitHub Actions `release` workflow re-runs the full test suite, then
GoReleaser builds all platforms reproducibly, generates SBOMs, writes
`checksums.txt`, signs it with keyless cosign (the GitHub workflow OIDC
identity — there is no long-lived signing key to rotate or leak), and
publishes the GitHub release. The workflow includes the editor extension
in the same release and publishes it to the marketplace when a token is
configured. It pushes the generated Homebrew formula to a version-specific
branch in `contrl-co/homebrew-tap` and opens a pull request against protected
`main`; the release fails if that PR is not observable. A CODEOWNER must review
the formula and its `Gate summary` must pass before Homebrew users receive the
update. The tap credential therefore needs only contents and pull-request write
access to that repository; it does not need or receive a branch-protection
bypass.

## Rollback playbook

If a released artifact is bad (miscompiles, non-determinism,
vulnerability, mis-signed):

1. **Stop the bleeding**: delete the GitHub release so
   `releases/latest` stops serving it (Releases → delete; the tag may stay
   for forensics).
2. **Homebrew**: revert the formula commit in the tap repository so
   `brew install crlc` resolves to the previous release.
3. **Marketplace**: unpublish the extension version
   (`npx @vscode/vsce unpublish` or the marketplace UI) if the
   extension is affected.
4. **Advise**: publish a release note on the deleted release's
   replacement and a `SECURITY.md`-linked advisory stating: affected
   versions, the failure, the checksums of the bad artifacts, and the
   fixed version. Consumers pin by checksum — give them the exact bad
   hashes to hunt for.
5. **Fix forward**: the next tag is a new patch version; never reuse
   or re-point a released tag — released (tag, checksum) pairs are
   immutable history, exactly like bundle hashes.

Go module proxies cache released versions forever; a bad version that
must not be used is communicated via `retract` in `go.mod` in the next
release.
