#!/usr/bin/env bash
# Usage: run_codex_review.sh <repo-dir> <base-ref> <context-file> <out-file>
# Runs codex in a read-only sandbox. The reviewer rules go in as developer
# instructions. The diff scope and context go in as the user prompt.
set -euo pipefail

repo=$1 base=$2 context=$3 out=$4
skill_dir=$(cd "$(dirname "$0")/.." && pwd)

# A JSON string is a valid TOML basic string, so -c parses it as one value.
instructions=$(jq -Rs . <"$skill_dir/references/codex-instructions.md")

merge_base=$(git -C "$repo" merge-base "$base" HEAD)

prompt=$(
	cat <<EOF
Review the change on the current branch.

Diff scope: \`git diff $merge_base...HEAD\` (base \`$base\`, merge base \`$merge_base\`).
Changed files:
$(git -C "$repo" diff --stat "$merge_base"...HEAD)

## Context from the ticket and planning

$(cat "$context")
EOF
)

codex exec \
	--ephemeral \
	--sandbox read-only \
	--color never \
	-C "$repo" \
	-c developer_instructions="$instructions" \
	-o "$out" \
	"$prompt" </dev/null >"$out.log" 2>&1

cat "$out"
