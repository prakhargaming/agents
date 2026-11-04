#!/usr/bin/env python3
import argparse
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
UPSTREAMS = {
    'pstack': ROOT / 'upstream/cursor-plugins',
    'ponytail': ROOT / 'upstream/ponytail',
}
CATALOG = ROOT / '.claude-plugin/marketplace.json'


def git(*args, cwd=ROOT):
    return subprocess.check_output(['git', '-C', str(cwd), *args], text=True).strip()


def discover(directory):
    return {p.name: p for p in sorted(directory.iterdir()) if (p / 'SKILL.md').is_file()}


def link_skills(home):
    shared = {}
    for directory in [UPSTREAMS['pstack'] / 'pstack/skills', UPSTREAMS['ponytail'] / 'skills', ROOT / 'plugins/personal/skills']:
        skills = discover(directory)
        overlap = shared.keys() & skills.keys()
        if overlap:
            raise SystemExit(f'Duplicate shared skills in {directory}: {sorted(overlap)}')
        shared.update(skills)
    links = {ROOT / 'skills' / name: source for name, source in shared.items()}
    for harness in ['claude', 'codex']:
        targets = {name: ROOT / 'skills' / name for name in shared}
        extra = discover(ROOT / f'plugins/{harness}/skills')
        if targets.keys() & extra.keys():
            raise SystemExit(f'Duplicate skills for {harness}')
        targets.update(extra)
        links.update({home / f'.{harness}/skills' / name: source for name, source in targets.items()})
    for dest, source in links.items():
        if dest.is_symlink():
            if dest.resolve() != source.resolve():
                raise SystemExit(f'Conflicting link: {dest} -> {os.readlink(dest)}')
        elif dest.exists():
            raise SystemExit(f'Refusing to replace existing path: {dest}')
    for dest, source in links.items():
        if not dest.is_symlink():
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.symlink_to(os.path.relpath(source, dest.parent), target_is_directory=True)
    for directory in [ROOT / 'skills', home / '.claude/skills', home / '.codex/skills']:
        for dest in directory.iterdir():
            if dest in links or not dest.is_symlink():
                continue
            target = dest.resolve()
            if target.is_relative_to(ROOT / 'skills') or any(target.is_relative_to(path) for path in [UPSTREAMS['pstack'] / 'pstack/skills', UPSTREAMS['ponytail'] / 'skills']):
                dest.unlink()
    print(f'Linked {len(shared)} shared skills and harness-specific skills.')


def main():
    parser = argparse.ArgumentParser(description='Install shared skills or update an upstream pin.')
    parser.add_argument('command', choices=['install', 'update'])
    parser.add_argument('--upstream', choices=UPSTREAMS, default='pstack', help='Upstream to update (default: pstack)')
    parser.add_argument('--home', type=Path, default=Path.home(), help='Harness home directory')
    args = parser.parse_args()
    for name, path in UPSTREAMS.items():
        if (path / '.git').exists() and git('status', '--porcelain', cwd=path):
            raise SystemExit(f'Commit or discard changes inside {name} before installation.')
        git('submodule', 'update', '--init', '--depth', '1', '--', str(path.relative_to(ROOT)))
    git('sparse-checkout', 'set', 'pstack', cwd=UPSTREAMS['pstack'])
    catalog = json.loads(CATALOG.read_text())
    for name, path in UPSTREAMS.items():
        plugin = next(p for p in catalog['plugins'] if p['name'] == name)
        if args.command == 'update' and args.upstream == name:
            git('fetch', '--depth', '1', 'origin', 'main', cwd=path)
            git('checkout', '--detach', 'FETCH_HEAD', cwd=path)
            plugin['source']['sha'] = git('rev-parse', 'HEAD', cwd=path)
            CATALOG.write_text(json.dumps(catalog, indent=2) + '\n')
        actual = git('rev-parse', 'HEAD', cwd=path)
        if actual != plugin['source']['sha']:
            raise SystemExit(f'The {name} marketplace pin and submodule differ. Restore both from the same commit.')
    link_skills(args.home.expanduser().resolve())
    if args.command == 'update':
        print(f'Review and commit .claude-plugin/marketplace.json and {UPSTREAMS[args.upstream].relative_to(ROOT)} together.')


if __name__ == '__main__':
    main()
