#!/usr/bin/env python3
import argparse
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
UPSTREAM = ROOT / 'upstream/cursor-plugins'
CATALOG = ROOT / '.claude-plugin/marketplace.json'


def git(*args, cwd=ROOT):
    return subprocess.check_output(['git', '-C', str(cwd), *args], text=True).strip()


def discover(directory):
    return {p.name: p for p in sorted(directory.iterdir()) if (p / 'SKILL.md').is_file()}


def link_skills(home):
    shared = discover(UPSTREAM / 'pstack/skills')
    personal = discover(ROOT / 'plugins/personal/skills')
    overlap = shared.keys() & personal.keys()
    if overlap:
        raise SystemExit(f'Personal skills conflict with upstream: {sorted(overlap)}')
    shared.update(personal)
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
            if target.is_relative_to(ROOT / 'skills') or target.is_relative_to(UPSTREAM / 'pstack/skills'):
                dest.unlink()
    print(f'Linked {len(shared)} shared skills and harness-specific skills.')


def main():
    parser = argparse.ArgumentParser(description='Install shared skills or update the pstack pin.')
    parser.add_argument('command', choices=['install', 'update'])
    parser.add_argument('--home', type=Path, default=Path.home(), help='Harness home directory')
    args = parser.parse_args()
    if (UPSTREAM / '.git').exists() and git('status', '--porcelain', cwd=UPSTREAM):
        raise SystemExit('Commit or discard changes inside the upstream checkout before installation.')
    git('submodule', 'update', '--init', '--depth', '1', '--', 'upstream/cursor-plugins')
    git('sparse-checkout', 'set', 'pstack', cwd=UPSTREAM)
    catalog = json.loads(CATALOG.read_text())
    pstack = next(p for p in catalog['plugins'] if p['name'] == 'pstack')
    if args.command == 'update':
        if git('status', '--porcelain', cwd=UPSTREAM):
            raise SystemExit('Commit or discard changes inside the upstream checkout before updating.')
        git('fetch', '--depth', '1', 'origin', 'main', cwd=UPSTREAM)
        git('checkout', '--detach', 'FETCH_HEAD', cwd=UPSTREAM)
        pstack['source']['sha'] = git('rev-parse', 'HEAD', cwd=UPSTREAM)
        CATALOG.write_text(json.dumps(catalog, indent=2) + '\n')
    actual = git('rev-parse', 'HEAD', cwd=UPSTREAM)
    if actual != pstack['source']['sha']:
        raise SystemExit('The marketplace pin and submodule differ. Restore both from the same commit.')
    link_skills(args.home.expanduser().resolve())
    if args.command == 'update':
        print('Review and commit .claude-plugin/marketplace.json and upstream/cursor-plugins together.')


if __name__ == '__main__':
    main()
