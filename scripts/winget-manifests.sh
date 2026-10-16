#!/bin/sh
# Writes the winget manifests for a published release into
# manifests/k/khanhtd36/latui/<version>, laid out as in microsoft/winget-pkgs,
# from the release's checksums.txt. Later releases are submitted by the
# release workflow's winget job; this is for the first one, or by hand.
#   scripts/winget-manifests.sh 0.1.0 [out-dir]
set -eu
version="${1:?version, such as 0.1.0}"
out="${2:-manifests}/k/khanhtd36/latui/$version"
repo="khanhtd36/latui"
id="khanhtd36.latui"
base="https://github.com/$repo/releases/download/v$version"
schema="1.12.0"

sums="$(curl -fsSL "$base/checksums.txt")"
sum() { echo "$sums" | awk -v f="latui_${version}_windows_$1.zip" '$2 == f { print toupper($1) }'; }
x64="$(sum x86_64)"
arm64="$(sum arm64)"
[ -n "$x64" ] && [ -n "$arm64" ] || { echo "no Windows zips in $base/checksums.txt" >&2; exit 1; }
date="$(curl -fsSL "https://api.github.com/repos/$repo/releases/tags/v$version" | grep -m1 '"published_at"' | sed -E 's/.*"([0-9]{4}-[0-9]{2}-[0-9]{2}).*/\1/')"

mkdir -p "$out"
cat > "$out/$id.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.version.$schema.schema.json

PackageIdentifier: $id
PackageVersion: $version
DefaultLocale: en-US
ManifestType: version
ManifestVersion: $schema
EOF

cat > "$out/$id.installer.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.installer.$schema.schema.json

PackageIdentifier: $id
PackageVersion: $version
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
- RelativeFilePath: latui.exe
  PortableCommandAlias: latui
Commands:
- latui
ReleaseDate: $date
Installers:
- Architecture: x64
  InstallerUrl: $base/latui_${version}_windows_x86_64.zip
  InstallerSha256: $x64
- Architecture: arm64
  InstallerUrl: $base/latui_${version}_windows_arm64.zip
  InstallerSha256: $arm64
ManifestType: installer
ManifestVersion: $schema
EOF

cat > "$out/$id.locale.en-US.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.defaultLocale.$schema.schema.json

PackageIdentifier: $id
PackageVersion: $version
PackageLocale: en-US
Publisher: khanhtd36
PublisherUrl: https://github.com/khanhtd36
PublisherSupportUrl: https://github.com/$repo/issues
Author: khanhtd36
PackageName: latui
PackageUrl: https://github.com/$repo
License: MIT
LicenseUrl: https://github.com/$repo/blob/main/LICENSE
Copyright: Copyright (c) 2026 Truong Duy Khanh
CopyrightUrl: https://github.com/$repo/blob/main/LICENSE
ShortDescription: Terminal UI for Atlassian Cloud notifications and Jira issues
Description: latui is a keyboard-first, lazygit-style terminal UI for Atlassian Cloud. It shows your Jira and Confluence notifications (Direct and Watching, with an unread filter and mark as read), opens Jira issues with their fields, description, comments and history, and transitions, comments on, relabels and reassigns them. It signs in with Atlassian's acli or an API token.
Moniker: latui
Tags:
- atlassian
- jira
- confluence
- notifications
- terminal
- tui
ReleaseNotesUrl: https://github.com/$repo/releases/tag/v$version
ManifestType: defaultLocale
ManifestVersion: $schema
EOF
echo "$out"
