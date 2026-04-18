#!/usr/bin/env bash
set -euo pipefail

build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
sha="${COMMIT_SHA:-${GITHUB_SHA:-$(git rev-parse HEAD)}}"
commit_sha="$(git rev-parse --short=12 "$sha")"
ref_type="${REF_TYPE:-${GITHUB_REF_TYPE:-branch}}"
ref_name="${REF_NAME:-${GITHUB_REF_NAME:-main}}"
run_number="${RUN_NUMBER:-${GITHUB_RUN_NUMBER:-0}}"

if [[ "$ref_type" == "tag" ]]; then
  release_tag="$ref_name"
  if [[ ! "$release_tag" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "Tag '$release_tag' is not semver vMAJOR.MINOR.PATCH" >&2
    exit 1
  fi
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  patch="${BASH_REMATCH[3]}"
  build="0"
  build_version="${major}.${minor}.${patch}"
  release_name="$release_tag"
  prerelease="false"
else
  major="0"
  minor="0"
  patch="0"
  build="$run_number"
  release_tag="dev-${commit_sha}"
  build_version="0.0.0-dev.${run_number}"
  release_name="dev-${commit_sha}"
  prerelease="true"
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
