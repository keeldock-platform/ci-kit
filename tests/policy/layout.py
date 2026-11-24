#!/usr/bin/env python3
"""Check the versioned public repository layout without executing project code."""
import argparse
import fnmatch
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

ROOT_FILES = {'README.md','CHANGELOG.md','LICENSE','go.mod','go.sum','package.json','package-lock.json','.gitignore','.gitattributes','.goreleaser.yaml','.goreleaser.yml','.ci-layout.json','NOTICE','THIRD_PARTY_NOTICES'}
ROOT_DIRS = {'.github','cmd','internal','planner','packs','schemas','fixtures','tests','docs','kit','ci','core-ci'}
LEGACY_REVISION = '17c2495a3860ec4ff1a85d257987f6515dbc0c98'
NEW_WORKFLOWS = {'dispatch.yml','source-ci.yml','canary-ci.yml','pr-lint.yml','scheduled.yml','validate.yml','pack.yml','ci-policy.yml','cli-candidate.yml'}
PLATFORMS = {'linux-debian','linux-arch','macos-arm64','macos-x64','windows-x64','windows-arm64','ios-simulator','android-emulator','web-linux','server-linux-x64','server-linux-arm64'}


def check(root, paths, legacy, owner_text=None, role="kit"):
    failures = []
    parents = set()
    for name in paths:
        p = PurePosixPath(name)
        if p.is_absolute() or '..' in p.parts:
            failures.append('invalid_path'); continue
        if p.parts[0] not in ROOT_DIRS and name not in ROOT_FILES:
            failures.append('misplaced_file')
        if role in {'caller','legacy-caller'} and p.parts[0] in {'cmd','internal','planner','packs','schemas','fixtures','tests','docs'} and p.name != 'README.md' and not (role == 'legacy-caller' and p.parts[0] == 'docs'):
            failures.append('caller_implementation_copy')
        if role in {'caller','legacy-caller'} and name.startswith('.github/actions/') and p.name != 'README.md':
            failures.append('caller_implementation_copy')
        if name in legacy:
            continue  # Existing lane is preserved; no new legacy-path files are accepted.
        if name.startswith('.github/workflows/') and len(p.parts) != 3:
            failures.append('misplaced_file')
        if name.startswith('.github/') and len(p.parts) == 2 and p.name not in {'README.md','CODEOWNERS','dependabot.yml','actionlint.yaml','zizmor.yml','zizmor.yaml','tool-pins.json','diagnostics.json'}:
            failures.append('misplaced_file')
        if name.startswith('.github/actions/') and len(p.parts) > 4:
            failures.append('misplaced_file')
        if name.startswith('cmd/') and len(p.parts) > 2 and p.parts[1] not in {'kd-ci','ci-kit','kd-ci-credential','ci-candidate'}:
            failures.append('misplaced_file')
        if name.startswith('internal/') and len(p.parts) > 2 and not re.fullmatch('[a-z][a-z0-9_]*',p.parts[1]):
            failures.append('misplaced_file')
        if name.startswith('schemas/') and len(p.parts) > 2:
            failures.append('misplaced_file')
        if name.startswith('tests/') and len(p.parts) > 2 and p.parts[1] not in {'policy','golden','integration','selection'}:
            failures.append('misplaced_file')
        if p.parts[0] in {'kit','ci','core-ci'} and role != 'legacy-caller':
            failures.append('new_legacy_file')
        if name.endswith('ci-unit.json') and not name.startswith('fixtures/synthetic/'):
            failures.append('public_unit_manifest')
        if p.suffix in {'.toml'} and '.unit.' in name:
            failures.append('duplicate_unit_format')
        for parent in p.parents:
            if str(parent) != '.' and not (role == 'legacy-caller' and p.parts[0] in {'kit','ci','core-ci'}): parents.add(str(parent))
        if name.startswith('.github/workflows/') and p.suffix in {'.yml','.yaml'}:
            if role == 'legacy-caller':
                if not re.fullmatch(r'[a-z][a-z0-9-]*\.ya?ml', p.name): failures.append('workflow_name')
            elif p.name not in NEW_WORKFLOWS and not (p.name.startswith('pack-') and p.name[5:-4] in PLATFORMS):
                failures.append('workflow_name')
        if name.startswith('.github/actions/') and p.name == 'action.yml':
            if len(p.parts) != 4 or not re.fullmatch('[a-z][a-z0-9]*(?:-[a-z0-9]+)*', p.parts[2]): failures.append('action_name')
        if name.startswith('packs/') and len(p.parts) > 2 and p.parts[1] not in PLATFORMS:
            failures.append('platform_name')
    for parent in sorted(parents):
        if role != 'legacy-caller' and parent + '/README.md' not in paths:
            failures.append('missing_readme')
    ownerpath = root / '.github/CODEOWNERS'
    owners = []
    if owner_text is None and ownerpath.is_file(): owner_text = ownerpath.read_text()
    if owner_text is not None:
        for line in owner_text.splitlines():
            if not line.strip() or line.lstrip().startswith('#'): continue
            fields = line.split()
            if len(fields) >= 2 and all(re.fullmatch(r'@[A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)?', s) for s in fields[1:]): owners.append(fields[0])
    required = ['/.github/','/cmd/','/internal/','/planner/','/packs/','/schemas/','/fixtures/','/tests/','/docs/','/.github/CODEOWNERS']
    if role != 'legacy-caller' and not all('*' in owners or s in owners for s in required):
        failures.append('ownership_unconfigured')
    return sorted(set(failures))


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--root', default='.')
    ap.add_argument('--head')
    ap.add_argument('--workflow-paths', action='store_true')
    ap.add_argument('--action-paths', action='store_true')
    args = ap.parse_args()
    try:
        root = Path(args.root).resolve(strict=True)
        if args.action_paths:
            raw = subprocess.check_output(['git','-C',str(root),'ls-files','-z']).decode()
            for name in sorted(raw.split('\0')):
                if PurePosixPath(name).name in {'action.yml','action.yaml'}: print(name)
            return 0
        if args.workflow_paths:
            for file in sorted(set((root / '.github/workflows').glob('*.yml')) | set((root / '.github/workflows').glob('*.yaml'))):
                print(file.relative_to(root).as_posix())
            return 0
        def git(*args):
            return subprocess.run(['git','--no-replace-objects','-C',str(root),*args],capture_output=True,check=True).stdout
        if args.head and not re.fullmatch('[a-f0-9]{40}',args.head): raise ValueError('invalid_revision')
        config = json.loads(git('show', args.head + ':.ci-layout.json') if args.head else (root / '.ci-layout.json').read_bytes())
        if config.get('schema_version') != 1: raise ValueError('layout_policy')
        if set(config) == {'schema_version','role'} and config['role'] in {'kit','caller','legacy-caller'}:
            legacy = set()
        elif set(config) == {'schema_version','legacy_revision'} and config['legacy_revision'] == LEGACY_REVISION:
            legacy = set(git('ls-tree','-rz','--name-only',LEGACY_REVISION).decode('utf-8').split('\0')) - {''}
        else: raise ValueError('layout_policy')
        raw = git('ls-tree','-rz','--name-only',args.head) if args.head else git('ls-files','-z','--cached')
        paths = set(raw.decode('utf-8').split('\0')) - {''}
        owner_text = git('show',args.head + ':.github/CODEOWNERS').decode('utf-8') if args.head else None
        failures = check(root, paths, legacy, owner_text, config.get("role", "kit"))
        print('FAIL layout ' + ','.join(failures) if failures else 'PASS layout')
        return int(bool(failures))
    except Exception:
        print('FAIL layout_policy')
        return 1

if __name__ == '__main__': sys.exit(main())
