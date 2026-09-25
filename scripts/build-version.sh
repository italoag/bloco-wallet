#!/usr/bin/env bash
set -euo pipefail

stable_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
tag_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'
ref_type=${BUILD_REF_TYPE:-${GITHUB_REF_TYPE:-}}
ref_name=${BUILD_REF_NAME:-${GITHUB_REF_NAME:-}}

if ! git rev-parse --verify HEAD >/dev/null 2>&1; then
  if [[ "$ref_type" == tag ]]; then
    printf '%s\n' 'Cannot resolve a release tag without Git history' >&2
    exit 1
  fi
  printf '%s\n' 'dev-SNAPSHOT'
  exit 0
fi

release_tag=''
case "$ref_type" in
  tag)
    if [[ ! "$ref_name" =~ $tag_pattern ]]; then
      printf '%s\n' 'Invalid release tag' >&2
      exit 1
    fi
    tag_commit=$(git rev-parse --verify "refs/tags/${ref_name}^{commit}")
    head_commit=$(git rev-parse HEAD)
    if [[ "$tag_commit" != "$head_commit" ]]; then
      printf '%s\n' 'Release tag does not match HEAD' >&2
      exit 1
    fi
    release_tag=$ref_name
    ;;
  branch)
    ;;
  '')
    if ! git symbolic-ref -q HEAD >/dev/null; then
      while IFS= read -r candidate; do
        if [[ "$candidate" =~ $tag_pattern ]]; then
          release_tag=$candidate
          break
        fi
      done < <(git tag --points-at HEAD --sort=-version:refname)
    fi
    ;;
  *)
    printf '%s\n' 'Unsupported build ref type' >&2
    exit 1
    ;;
esac

if [[ -n "$release_tag" && -z "$(git status --porcelain --untracked-files=normal)" ]]; then
  printf '%s\n' "$release_tag"
  exit 0
fi

base=''
while IFS= read -r candidate; do
  if [[ "$candidate" =~ $stable_pattern ]]; then
    base=$candidate
    break
  fi
done < <(git tag --merged HEAD --sort=-version:refname)

if [[ -z "$base" ]]; then
  printf '%s\n' 'v0.1.0-SNAPSHOT'
  exit 0
fi

if [[ "$base" =~ $stable_pattern ]]; then
  major=${BASH_REMATCH[1]}
  minor=${BASH_REMATCH[2]}
  printf 'v%s.%s.0-SNAPSHOT\n' "$major" "$((minor + 1))"
fi
