#!/usr/bin/env bash
set -euo pipefail

: "${TAG:?TAG must name the published release}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must name the repository}"

[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || exit 0
latest="$(gh api "repos/$GITHUB_REPOSITORY/releases/latest" --jq .tag_name)"
if [ "$latest" != "$TAG" ]; then
  echo "Keeping stable: $TAG is not the latest published release ($latest)"
  exit 0
fi

release_commit="$(git rev-parse "refs/tags/$TAG^{commit}")"
test "$release_commit" = "$(git rev-parse HEAD)"
git cat-file -e "$release_commit:flake.nix"
git cat-file -e "$release_commit:flake.lock"

old="$(git ls-remote origin refs/heads/stable | cut -f1)"
if [ -n "$old" ]; then
  git fetch origin refs/heads/stable
  current="$(git show "$old:internal/buildinfo/buildinfo.go" | sed -n 's/^const Version = "\([^"]*\)"$/\1/p')"
  test -n "$current"
  newest="$(printf '%s\n' "$current" "${TAG#v}" | sort -V | tail -n1)"
  if [ "$newest" != "${TAG#v}" ]; then
    echo "Keeping stable: $TAG is older than $current"
    exit 0
  fi
fi

git push --force-with-lease="refs/heads/stable:$old" origin "$release_commit:refs/heads/stable"
remote="$(git ls-remote origin refs/heads/stable | cut -f1)"
test "$remote" = "$release_commit"
