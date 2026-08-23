#!/usr/bin/env bash
set -euo pipefail

build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
sha="${COMMIT_SHA:-${GITHUB_SHA:-$(git rev-parse HEAD)}}"
commit_sha="$(git rev-parse --short=12 "$sha")"
ref_type="${REF_TYPE:-${GITHUB_REF_TYPE:-branch}}"
ref_name="${REF_NAME:-${GITHUB_REF_NAME:-main}}"

# A release is only ever cut from a version tag -- vMAJOR.MINOR.PATCH for a
# final release, or vMAJOR.MINOR.PATCH-rc / -rcN for a release candidate
# (bare "-rc" and numbered "-rc1", "-rc2", ... are both accepted). Anything
# else (a branch push, or workflow_dispatch run from a branch) is refused
# rather than falling back to an auto-generated dev prerelease.
if [[ "$ref_type" != "tag" ]]; then
  echo "release-main only runs from a version tag (vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-rcN); got ref_type='$ref_type' ref_name='$ref_name'." >&2
  exit 1
fi

release_tag="$ref_name"
if [[ ! "$release_tag" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)(-rc([0-9]*))?$ ]]; then
  echo "Tag '$release_tag' is not semver vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-rcN" >&2
  exit 1
fi
major="${BASH_REMATCH[1]}"
minor="${BASH_REMATCH[2]}"
patch="${BASH_REMATCH[3]}"
rc_suffix="${BASH_REMATCH[4]}"  # "", "-rc", or "-rcN"
rc_num="${BASH_REMATCH[5]}"     # "" unless "-rcN"
build="${rc_num:-0}"
build_version="${major}.${minor}.${patch}${rc_suffix}"
release_name="$release_tag"
if [[ -n "$rc_suffix" ]]; then
  prerelease="true"
else
  prerelease="false"
fi

echo "release_tag=$release_tag"
echo "release_name=$release_name"
echo "build_version=$build_version"
echo "build_date=$build_date"
echo "commit_sha=$commit_sha"
echo "prerelease=$prerelease"
echo "major=$major"
echo "minor=$minor"
echo "patch=$patch"
echo "build=$build"
