#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
	printf 'Usage: %s <repo-dir> <base-ref> <context-file> <out-file>\n' "$0" >&2
	exit 2
fi

repo=$(cd "$1" && pwd)
base=$2
context=$(realpath "$3")
out=$(realpath -m "$4")
skill_dir=$(cd "$(dirname "$0")/.." && pwd)
command -v claude >/dev/null
[[ -r "$context" ]]
head=$(git -C "$repo" rev-parse HEAD)
merge_base=$(git -C "$repo" merge-base "$base" "$head")

prompt=$(
	cat <<PROMPT
Review the change on the current branch.

Diff scope: merge base $merge_base to commit $head (base $base).
Changed files:
$(git -C "$repo" diff --no-ext-diff --no-textconv --stat "$merge_base" "$head")

## Context from the ticket and planning

$(cat "$context")

## Committed diff

$(git -C "$repo" diff --no-ext-diff --no-textconv "$merge_base" "$head")
PROMPT
)

cd "$repo"
if printf '%s\n' "$prompt" | claude --print \
	--safe-mode \
	--no-session-persistence \
	--output-format text \
	--permission-mode dontAsk \
	--tools 'Read,Glob,Grep' \
	--allowedTools 'Read,Glob,Grep' \
	--strict-mcp-config \
	--mcp-config '{"mcpServers":{}}' \
	--append-system-prompt "$(cat "$skill_dir/references/claude-instructions.md")" \
	>"$out" 2>"$out.log"; then
	cat "$out"
else
	status=$?
	printf 'Claude review failed (exit %s). See %s and %s.log\n' "$status" "$out" "$out" >&2
	exit "$status"
fi
