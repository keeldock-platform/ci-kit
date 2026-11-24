#!/usr/bin/env python3
"""Reject mutable execution refs independently of native lock enforcement."""
import argparse
import json
from pathlib import Path
import re
import sys
import subprocess

def immutable_ref(ref):
    if re.fullmatch(r'\$/.github/actions/[a-z][a-z0-9-]*',ref): return True
    if re.fullmatch(r'\./.github/workflows/[a-z][a-z0-9-]*\.yml',ref): return True
    return bool(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[^@\s]+)?@[a-f0-9]{40}',ref))

def check(root, kit_sha=None):
    root = root.resolve()
    role = json.loads((root/'.ci-layout.json').read_text()).get('role','kit')
    caller = role in {'caller','legacy-caller'}
    if caller and not re.fullmatch('[a-f0-9]{40}',kit_sha or ''): return False
    files = sorted(set((root/'.github/workflows').glob('*.y*ml')) |
                   set((root/'.github/actions').rglob('action.y*ml')) |
                   (set((root/'kit').rglob('action.y*ml')) if role == 'legacy-caller' else set()))
    if not files: return False
    tracked = set(subprocess.check_output(['git','-C',str(root),'ls-files','-z']).decode().split('\0')) if role == 'legacy-caller' else set()
    for file in files:
        text = file.read_text()
        current = caller and (role == 'caller' or file.relative_to(root).as_posix() in {'.github/workflows/source-ci.yml','.github/workflows/pr-lint.yml'})
        for ref in re.findall(r'^\s*(?:-\s*)?uses:\s*([^\s#]+)',text,re.M):
            local = role == 'legacy-caller' and re.fullmatch(r'(?:\./|\$/)kit/[a-zA-Z0-9_/-]+', ref)
            if local:
                target = root / ref[2:]
                if not target.is_dir() or target.is_symlink() or not target.resolve().is_relative_to(root): return False
                metadata = [target/n for n in ('action.yml','action.yaml') if (target/n).is_file()]
                if len(metadata) != 1 or metadata[0].relative_to(root).as_posix() not in tracked: return False
            elif not immutable_ref(ref): return False
            if current and ref not in {'keeldock-platform/ci-kit/.github/workflows/ci-policy.yml@'+kit_sha, 'keeldock-platform/ci-kit/.github/workflows/validate.yml@'+kit_sha}: return False
        if current:
            values = re.findall(r'^\s+kit_sha:\s*(\S+)',text,re.M)
            if values != [kit_sha]: return False
    if role == 'legacy-caller' and not all((root/'.github/workflows'/name).is_file() for name in ('source-ci.yml','pr-lint.yml')): return False
    return True

if __name__=='__main__':
    ap=argparse.ArgumentParser();ap.add_argument('--root',default='.');ap.add_argument('--kit-sha');args=ap.parse_args()
    ok=check(Path(args.root),args.kit_sha);print('PASS immutable_actions' if ok else 'FAIL immutable_actions');sys.exit(0 if ok else 1)
