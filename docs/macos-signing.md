# macOS release signing and notarization

Versioned `server/v*` releases always publish checksum-protected raw macOS
binaries for Intel and Apple silicon. When the repository variable
`MACOS_NOTARIZATION_ENABLED` is `true`, the same release also publishes signed,
notarized, stapled `.pkg` installers. The `server/latest` workflow waits for the
versioned release, verifies the exact configured 14- or 16-asset manifest and
all six binary checksums, then mirrors the same assets. It never rebuilds a
second set of macOS artifacts.

The release workflow follows Apple's [custom notarization
workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow)
and GitHub's guidance for [deployment environments and protected
secrets](https://docs.github.com/en/actions/concepts/workflows-and-actions/deployment-environments).

## Required Apple credentials

Create and retain these credentials in the Apple Developer and App Store
Connect accounts used for releases:

1. A **Developer ID Application** certificate and private key, exported from
   Keychain Access as a password-protected PKCS#12 (`.p12`) file. It signs the
   command-line binary with the hardened runtime and a trusted timestamp.
2. A **Developer ID Installer** certificate and private key, also exported as a
   password-protected `.p12`. It signs the flat installer package.
3. An App Store Connect API key (`AuthKey_<KEY_ID>.p8`) that can submit software
   to Apple's notary service, plus its key ID and issuer ID.

Keep the original files in the team's approved secret manager. Never commit
certificates, private keys, passwords, API keys, or their base64 encodings.

## GitHub environment setup

Create a GitHub Actions environment named `macos-release-signing`. Restrict its
deployment branches and tags to the intended release policy and add required
reviewers if the repository plan supports them. Store identity names as
environment variables:

- `APPLE_DEVELOPER_ID_APPLICATION_IDENTITY`, for example `Developer ID
  Application: Example Company (TEAMID)`
- `APPLE_DEVELOPER_ID_INSTALLER_IDENTITY`, for example `Developer ID Installer:
  Example Company (TEAMID)`

Store these values as environment secrets:

- `APPLE_DEVELOPER_ID_APPLICATION_P12_BASE64`
- `APPLE_DEVELOPER_ID_APPLICATION_P12_PASSWORD`
- `APPLE_DEVELOPER_ID_INSTALLER_P12_BASE64`
- `APPLE_DEVELOPER_ID_INSTALLER_P12_PASSWORD`
- `APPLE_NOTARY_KEY_P8_BASE64`
- `APPLE_NOTARY_KEY_ID`
- `APPLE_NOTARY_ISSUER_ID`

After all variables and secrets are configured, create the repository variable
`MACOS_NOTARIZATION_ENABLED` with the value `true`. Leave it absent or set it to
`false` to publish checksum-protected raw binaries without `.pkg` installers.
The release workflow never silently falls back from a requested notarized build:
when the variable is `true`, missing or invalid Apple credentials fail the
release.

Generate the three base64 values on a trusted Mac without adding line breaks:

```bash
base64 < DeveloperIDApplication.p12 | tr -d '\n' | pbcopy
base64 < DeveloperIDInstaller.p12 | tr -d '\n' | pbcopy
base64 < AuthKey_KEYID.p8 | tr -d '\n' | pbcopy
```

Paste each clipboard value directly into the matching GitHub environment
secret. GitHub documents the operational limits of [Actions
secrets](https://docs.github.com/en/actions/reference/security/secrets); do not
print or transform secret values in workflow logs.

For every macOS matrix job, the workflow decodes credentials into runner
temporary storage, imports both identities into a random-password temporary
keychain, validates the exact configured identities, and stores the API key as
a validated `notarytool` keychain profile. Decoded source files are deleted as
soon as import finishes, and the temporary keychain is deleted even when a
later step fails.

## Local release-path validation

Use a throwaway keychain so local validation exercises the same isolation as
CI. The example assumes the certificates already exist as `.p12` files and the
API key exists as a `.p8` file:

```bash
KEYCHAIN="$TMPDIR/memento-signing.keychain-db"
KEYCHAIN_PASSWORD="$(openssl rand -hex 32)"
security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
security set-keychain-settings -lut 21600 "$KEYCHAIN"
security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
security import DeveloperIDApplication.p12 -k "$KEYCHAIN" -f pkcs12 \
  -P "$APPLICATION_P12_PASSWORD" -T /usr/bin/codesign -T /usr/bin/pkgbuild
security import DeveloperIDInstaller.p12 -k "$KEYCHAIN" -f pkcs12 \
  -P "$INSTALLER_P12_PASSWORD" -T /usr/bin/codesign -T /usr/bin/pkgbuild
security set-key-partition-list -S apple-tool:,apple: -s \
  -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN"
security list-keychains -d user -s "$KEYCHAIN"
xcrun notarytool store-credentials memento-notary-local \
  --key AuthKey_KEYID.p8 \
  --key-id "$APPLE_NOTARY_KEY_ID" \
  --issuer "$APPLE_NOTARY_ISSUER_ID" \
  --keychain "$KEYCHAIN" \
  --validate
```

Build the native binary, then run the same signing helper as CI:

```bash
make build
VERSION=0.0.0-test
./scripts/macos-sign-and-notarize.sh \
  --binary bin/memento-mcp \
  --package "dist/memento-mcp_${VERSION}_darwin_$(go env GOARCH).pkg" \
  --version "$VERSION" \
  --application-identity "$APPLE_DEVELOPER_ID_APPLICATION_IDENTITY" \
  --installer-identity "$APPLE_DEVELOPER_ID_INSTALLER_IDENTITY" \
  --keychain "$KEYCHAIN" \
  --notary-profile memento-notary-local
security delete-keychain "$KEYCHAIN"
```

The helper fails unless code signing, package signing, notarization acceptance,
ticket stapling, staple validation, `pkgutil --check-signature`, and Gatekeeper
assessment all succeed. Apple documents the complete submission lifecycle in
[Notarizing macOS software before
distribution](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution).

## Troubleshooting

Inspect identities before retrying a failed build:

```bash
security find-identity -v -p codesigning "$KEYCHAIN"
security find-identity -v -p basic "$KEYCHAIN"
codesign --verify --strict --verbose=4 bin/memento-mcp
pkgutil --check-signature dist/memento-mcp_VERSION_darwin_ARCH.pkg
spctl --assess --type install --verbose=4 dist/memento-mcp_VERSION_darwin_ARCH.pkg
```

- **Identity not found:** confirm the `.p12` contains its private key, the
  configured identity matches `security find-identity` exactly, and the
  application and installer certificates were not swapped.
- **User interaction is not allowed:** unlock the temporary keychain and rerun
  `security set-key-partition-list` with `apple-tool:,apple:` access.
- **Timestamp or hardened-runtime rejection:** inspect `codesign -dvvv`; the
  binary must have `runtime` options and a secure timestamp, and the package
  must have an installer timestamp.
- **Notarization rejected:** obtain the submission ID from the workflow output,
  then run `xcrun notarytool log <ID> --keychain-profile <PROFILE> --keychain
  <KEYCHAIN> rejection.json`. The helper automatically prints this log when
  Apple returns a non-accepted result. Apple's [common notarization issue
  guide](https://developer.apple.com/documentation/security/resolving-common-notarization-issues)
  explains the usual code-signing and bundle failures.
- **Stapling fails after acceptance:** retry `xcrun stapler staple -v <pkg>`,
  verify network access to Apple, and confirm the submitted file is byte-for-byte
  identical to the file being stapled.
- **`server/latest` times out:** inspect the versioned release first. It must
  contain exactly six binaries, six matching `.sha256` sidecars, and two `.deb`
  packages. When `MACOS_NOTARIZATION_ENABLED=true`, it must also contain two
  notarized `.pkg` packages. The latest workflow deliberately rejects partial
  or extra manifests.

## Rotation and release verification

Rotate one credential family at a time. Export and escrow the replacement,
update the corresponding environment secrets or variables, and publish a
versioned test release. From a clean Mac that does not contain the signing
certificates, download both architecture packages and verify them with
`pkgutil --check-signature`, `spctl --assess --type install`, and an actual
installer run. Confirm that `server/latest` contains the exact same asset names
and checksums before revoking the old certificate or API key.

The implementation and credential-free contract tests can run in ordinary CI,
but the first credentialed `server/v*` tag after setup or rotation is the final
external validation of the Apple account, certificate chain, and notary access.
