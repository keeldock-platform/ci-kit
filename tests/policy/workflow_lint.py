#!/usr/bin/env python3
"""Run actionlint with a lint-only projection of documented native syntax."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys


def project(text, action_lines=()):
    if sum(line.startswith("cache-mode:") for line in text.splitlines()) > 1:
        raise ValueError("duplicate native cache policy")
    lines = []
    for number, line in enumerate(text.splitlines(keepends=True), 1):
        # Keep line numbers intact. This is the sole accepted cache authority.
        if line.rstrip('\r\n') == 'cache-mode: none':
            lines.append('\n')
            continue
        # For static input checking the same action metadata is used. Published
        # workflows retain $/ resolution at their owning workflow revision.
        if number in action_lines:
            line = re.sub(r'^(\s*(?:-\s+)?uses:\s+)\$/([a-zA-Z0-9_.-]+(?:/[a-zA-Z0-9_.-]+)+)(\s*(?:#.*)?)$', r'\1./\2\3', line)
        lines.append(line)
    return ''.join(lines)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--actionlint', required=True)
    ap.add_argument('--root', default='.')
    args = ap.parse_args()
    root = Path(args.root).resolve(strict=True)
    tool = Path(args.actionlint)
    if not tool.is_absolute() or not tool.is_file():
        print('FAIL workflow_lint_tool')
        return 1
    files = sorted(set((root / '.github/workflows').glob('*.yml')) | set((root / '.github/workflows').glob('*.yaml')))
    if not files:
        print('FAIL workflow_lint_empty')
        return 1
    native = []
    unchanged = []
    try:
        raw = subprocess.run([str(tool), '-format', '{{json .}}'], cwd=root, capture_output=True, timeout=110)
        diagnostics = json.loads(raw.stdout or b'[]')
        if not isinstance(diagnostics, list) or raw.returncode not in (0, 1):
            raise ValueError('invalid linter response')
        action_lines = {}
        cache_message = 'unexpected key "cache-mode" for "workflow" section. expected one of "concurrency", "defaults", "env", "jobs", "name", "on", "permissions", "run-name"'
        for diagnostic in diagnostics:
            file = Path(diagnostic['filepath'])
            file = file if file.is_absolute() else root / file
            if file not in files: raise ValueError('unknown diagnostic file')
            line_number = diagnostic['line']
            line = file.read_text().splitlines()[line_number - 1]
            if diagnostic['kind'] == 'syntax-check' and diagnostic['message'] == cache_message and line == 'cache-mode: none':
                continue
            valid_action = False
            for metadata in [*root.rglob('action.yml'), *root.rglob('action.yaml')]:
                ref = '$/' + metadata.parent.relative_to(root).as_posix()
                message = 'specifying action "' + ref + '" in invalid format because ref is missing. available formats are "{owner}/{repo}@{ref}" or "{owner}/{repo}/{path}@{ref}"'
                if diagnostic['kind'] == 'action' and diagnostic['message'] == message and re.fullmatch(r'\s*(?:-\s+)?uses:\s+' + re.escape(ref) + r'\s*(?:#.*)?', line):
                    action_lines.setdefault(file, set()).add(line_number)
                    valid_action = True
            if not valid_action: raise ValueError('unhandled actionlint diagnostic')
        if raw.returncode and not diagnostics: raise ValueError('missing linter diagnostics')
        for file in files:
            original = file.read_text(encoding='utf-8')
            projected = project(original, action_lines.get(file, ()))
            (native if projected != original else unchanged).append((file, projected))
        # The initial JSON invocation already validated every unchanged workflow.
        # Re-run only native projections after accounting for every diagnostic.
        for file, text in native:
            name = file.relative_to(root).as_posix()
            p = subprocess.run([str(tool), '-stdin-filename', name, '-'], input=text.encode(), cwd=root, capture_output=True, timeout=120)
            if p.returncode:
                print('FAIL workflow_lint')
                return 1
    except Exception:
        print('FAIL workflow_lint_tool')
        return 1
    print('PASS workflow_lint')
    return 0

if __name__ == '__main__': sys.exit(main())
