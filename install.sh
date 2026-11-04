#!/bin/sh
# Install the skills CLI and link the skills into each harness.
# Run: gh api repos/prakhargaming/agents/contents/install.sh -H 'Accept: application/vnd.github.raw' | sh
set -eu

repo=prakhargaming/agents
root=${AGENTS_HOME:-$HOME/.agents}
bindir=$HOME/.local/bin

if ! command -v gh >/dev/null 2>&1; then
  echo "install.sh needs the GitHub CLI (gh). Install it, then run gh auth login." >&2
  exit 1
fi
if ! gh auth status >/dev/null 2>&1; then
  echo "gh is not authenticated. Run gh auth login first." >&2
  exit 1
fi

case $(uname -s) in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac

# skills install does a shallow submodule checkout, so the clone skips them.
if [ ! -d "$root/.git" ]; then
  gh repo clone "$repo" "$root"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
asset="skills_${os}_${arch}.tar.gz"
gh release download --repo "$repo" --pattern "$asset" --dir "$tmp"
mkdir -p "$root/bin" "$bindir"
tar -xzf "$tmp/$asset" -C "$root/bin" skills
ln -sf "$root/bin/skills" "$bindir/skills"

AGENTS_HOME=$root "$root/bin/skills" install

case ":$PATH:" in
  *":$bindir:"*) ;;
  *) echo "Add $bindir to your PATH to run skills." ;;
esac
