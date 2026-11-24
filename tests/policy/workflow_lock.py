#!/usr/bin/env python3
"""Verify complete native locks while retaining mandatory full-SHA execution refs."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
from immutable_actions import check


def accepted(result):
    if not isinstance(result, dict) or result.get('valid') is not True or not isinstance(result.get('findings'), list): return False
    for finding in result['findings']:
        if finding.get('severity') == 'error': return False
        if finding.get('severity') == 'warning':
            # The tool recommends tags for traceability. Full immutable SHA refs
            # are deliberately stronger here and checked independently first.
            if finding.get('category') != 'sha-as-ref' or not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[a-f0-9]{40}', finding.get('dependency','')): return False
        elif finding.get('severity') not in {'info'}: return False
    return True


def main():
    ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('--root',default='.');ap.add_argument('--tool',required=True);ap.add_argument('--kit-sha')
    args=ap.parse_args()
    try:
        root=Path(args.root).resolve(strict=True);tool=Path(args.tool)
        if not tool.is_absolute() or not tool.is_file() or not check(root,args.kit_sha): raise ValueError()
        run=subprocess.run([str(tool),'--verify','--json=valid,findings'],cwd=root,capture_output=True,timeout=110)
        ok=run.returncode == 0 and accepted(json.loads(run.stdout))
    except Exception: ok=False
    print('PASS workflow_lock' if ok else 'FAIL workflow_lock')
    return 0 if ok else 1

if __name__ == '__main__': sys.exit(main())
