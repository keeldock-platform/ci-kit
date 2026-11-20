#!/usr/bin/env python3
"""Reject mutable execution refs independently of native lock enforcement."""
import argparse
import json
from pathlib import Path
import re
import sys

def immutable_ref(ref):
    if re.fullmatch(r'\$/.github/actions/[a-z][a-z0-9-]*',ref): return True
    if re.fullmatch(r'\./.github/workflows/[a-z][a-z0-9-]*\.yml',ref): return True
    return bool(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[^@\s]+)?@[a-f0-9]{40}',ref))

def check(root, kit_sha=None):
    role = json.loads((root/'.ci-layout.json').read_text()).get('role','kit')
    if role == 'caller' and not re.fullmatch('[a-f0-9]{40}',kit_sha or ''): return False
    files = list((root/'.github/workflows').glob('*.y*ml')) + list((root/'.github/actions').glob('*/action.y*ml'))
    if not files: return False
    for file in files:
        for ref in re.findall(r'^\s*(?:-\s*)?uses:\s*([^\s#]+)',file.read_text(),re.M):
            if not immutable_ref(ref): return False
            if role == 'caller' and ref != 'keeldock-platform/ci-kit/.github/workflows/ci-policy.yml@'+kit_sha and ref != 'keeldock-platform/ci-kit/.github/workflows/validate.yml@'+kit_sha: return False
        if role == 'caller':
            for value in re.findall(r'^\s+kit_sha:\s*(\S+)',file.read_text(),re.M):
                if value != kit_sha: return False
    return True

if __name__=='__main__':
    ap=argparse.ArgumentParser();ap.add_argument('--root',default='.');ap.add_argument('--kit-sha');args=ap.parse_args()
    ok=check(Path(args.root),args.kit_sha);print('PASS immutable_actions' if ok else 'FAIL immutable_actions');sys.exit(0 if ok else 1)
