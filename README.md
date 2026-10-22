# Personal agent skills

Private skills marketplace for `prakhargaming`. Personal skills live in this repository.
Pstack comes from the pinned `cursor/plugins` Git submodule. Its files are not copied or edited here.

The Claude marketplace is one adapter. Agents that read `SKILL.md` can use the same files through symlinks.
Harness-specific tools and frontmatter still require support from the harness.

## Install shared skills

Requires Git and Python 3.9 or later. Authenticate with GitHub before cloning this private repository.

```sh
git clone --recurse-submodules git@github.com:prakhargaming/agents.git ~/.agents
python3 ~/.agents/scripts/skills.py install
```

The installer creates `~/.agents/skills` as a generated index of symlinks.
It links shared skills into `~/.claude/skills` and `~/.codex/skills`.
It also links `codex-review` for Claude and `claude-review` for Codex.
Existing bundled skills and synced skills remain in place. A conflicting path stops installation.
Restart your agent session after installation so it can discover the skills.

For another harness, link the directories in `~/.agents/skills` into its personal skills directory.
The harness must support skill directories and symlinks. Remote machines need their own checkout.

## Update pstack

```sh
cd ~/.agents
python3 scripts/skills.py update
git diff --submodule=log
git add .claude-plugin/marketplace.json upstream/cursor-plugins
git commit -m "Update pstack"
git push
```

The update fetches `main` directly from `cursor/plugins` and refreshes the links.
Commit the submodule and marketplace pins together. Review upstream changes before you use the new skills.
On another machine, run `git pull --ff-only`, then run the installer.

To roll back an update, revert its commit and run the installer again.
No fork is needed unless you want to maintain changes to pstack itself.
Keep personal additions in `plugins/personal/skills/<name>/SKILL.md`.
Use a unique name; the installer rejects collisions with upstream.

## Use the Claude marketplace

The catalog follows the [Claude marketplace format](https://code.claude.com/docs/en/plugins/create-marketplace).
Its pstack entry uses a pinned `git-subdir` source, so Claude fetches the plugin directly from upstream.
The other entries contain personal shared skills and optional harness-specific review skills.

```sh
claude plugin marketplace add prakhargaming/agents
claude plugin install pstack@prakhar-skills
claude plugin install prakhar-personal@prakhar-skills
claude plugin install review-with-codex@prakhar-skills
```

Choose either marketplace installation or the symlink installer for Claude.
Installing both exposes duplicate skills with different invocation names.
The current machine uses symlinks; its marketplace is registered for discovery.
Marketplace skills use names such as `/pstack:how`; symlink skills retain `/how`.
After a catalog update, refresh the marketplace and update each installed plugin:

```sh
claude plugin marketplace update prakhar-skills
claude plugin update pstack@prakhar-skills
```

## Repository layout

- `.claude-plugin/marketplace.json`: personal plugin catalog and upstream revision.
- `plugins/personal/skills`: shared personal skills.
- `plugins/claude/skills`: skills intended for Claude.
- `plugins/codex/skills`: skills intended for Codex.
- `upstream/cursor-plugins`: unmodified upstream Git submodule.
- `skills`: generated symlinks, excluded from Git.
- `scripts/skills.py`: install links and update pstack.

The initial migration preserved local skill folders in a backup beside this checkout.
The former `poteto-help` copy omitted the new `statechart` entry; both now come from upstream.
Upstream pstack retains its MIT license in `upstream/cursor-plugins/pstack/LICENSE`.
