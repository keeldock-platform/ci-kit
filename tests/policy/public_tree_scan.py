#!/usr/bin/env python3
"""Public-safe credential and local-path scan over the full tree and bounded full history."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
from public_content import views, MAX_FILE, MAX_COMMITS

# Match complete credential shapes and local user paths, not GitHub's /home/runner/ path.
PATTERNS = [
    re.compile(r'(?<![a-z0-9])gh[pousr]_[a-z0-9_]{20,}(?![a-z0-9])'),
    re.compile(r'(?<![a-z0-9])github_pat_[a-z0-9_]{20,}(?![a-z0-9])'),
    re.compile(r'(?<![a-z0-9])(?:akia|asia)[a-z0-9]{16}(?![a-z0-9])'),
    re.compile(r'(?<![a-z0-9])xox[baprs]-[a-z0-9-]{16,}(?![a-z0-9])'),
    re.compile(r'-----begin (?:[a-z0-9 ]{0,64})private key-----\s+[a-z0-9+/=\s]{64,}\s+-----end '),
    re.compile(r'/(?:users|home)/(?!runner/)[a-z0-9._-]+/(?:[^\s\x22\x27]+)'),
]
BINARY_DIGESTS = json.loads((Path(__file__).with_name('public_binary_fixtures.json')).read_text())


def _git(root, *args):
    return subprocess.check_output(['git', '--no-replace-objects', '-C', str(root), *args], stderr=subprocess.DEVNULL)


def _check_bytes(data, path=None):
    try:
        texts = views(data)
    except (ValueError, UnicodeError):
        if path is not None and hashlib.sha256(data).hexdigest() == BINARY_DIGESTS.get(path):
            return None
        return 'uninspectable_content'
    if any(pattern.search(text) for pattern in PATTERNS for text in texts):
        return 'credential_or_local_path'
    return None


def scan(root):
    root = root.resolve(strict=True)
    if _git(root, 'rev-parse', '--is-shallow-repository').strip() != b'false':
        return ['shallow_history']
    try:
        head = _git(root, 'rev-parse', '--verify', 'HEAD').decode().strip()
    except subprocess.CalledProcessError:
        return ['missing_history']
    if not re.fullmatch(r'[a-f0-9]{40}', head):
        return ['missing_history']
    stages = _git(root, 'ls-files', '--stage', '-z')
    findings = []
    blob_cache = {}
    path_cache = {}
    seen_trees = set()

    def inspect_path(raw):
        if raw not in path_cache:
            path_cache[raw] = _check_bytes(raw)
        return path_cache[raw]

    def inspect_blob(oid, path):
        if oid not in blob_cache:
            size = int(_git(root, 'cat-file', '-s', oid))
            if size > MAX_FILE:
                blob_cache[oid] = ('size_limit', None)
            else:
                data = _git(root, 'cat-file', 'blob', oid)
                try:
                    texts = views(data)
                    hit = 'credential_or_local_path' if any(p.search(t) for p in PATTERNS for t in texts) else None
                    blob_cache[oid] = (hit, None)
                except (ValueError, UnicodeError):
                    blob_cache[oid] = ('uninspectable_content', hashlib.sha256(data).hexdigest())
        reason, digest = blob_cache[oid]
        if reason == 'uninspectable_content' and digest == BINARY_DIGESTS.get(path):
            return None
        return reason

    for stage in stages.split(b'\0'):
        if not stage:
            continue
        meta, raw = stage.split(b'\t', 1)
        mode, oid, index = meta.split()
        path = raw.decode('utf-8', 'strict')
        if mode not in {b'100644', b'100755'} or index != b'0':
            findings.append('unsupported_entry')
            continue
        path_hit = inspect_path(raw)
        if path_hit:
            findings.append(path_hit)
        blob_hit = inspect_blob(oid.decode(), path)
        if blob_hit:
            findings.append(blob_hit)
        current = root / path
        if not current.is_file() or current.is_symlink() or not current.resolve().is_relative_to(root):
            findings.append('unsupported_entry')
            continue
        if current.stat().st_size > MAX_FILE:
            findings.append('size_limit')
            continue
        wt_hit = _check_bytes(current.read_bytes(), path)
        if wt_hit:
            findings.append(wt_hit)

    revisions = _git(root, 'rev-list', '--max-count=' + str(MAX_COMMITS + 1), head).decode().splitlines()
    if len(revisions) > MAX_COMMITS:
        findings.append('commit_limit')
        return findings

    def inspect_tree(tree_oid, prefix=''):
        key = (tree_oid, prefix)
        if key in seen_trees:
            return
        seen_trees.add(key)
        for row in _git(root, 'ls-tree', '-z', tree_oid).split(b'\0'):
            if not row:
                continue
            meta, name_bytes = row.split(b'\t', 1)
            mode, kind, oid = meta.split()
            rel = (prefix + '/' + name_bytes.decode('utf-8', 'strict')) if prefix else name_bytes.decode('utf-8', 'strict')
            rel_bytes = rel.encode('utf-8')
            path_hit = inspect_path(rel_bytes)
            if path_hit:
                findings.append(path_hit)
            if kind == b'tree':
                inspect_tree(oid.decode(), rel)
            elif kind == b'blob':
                blob_hit = inspect_blob(oid.decode(), rel)
                if blob_hit:
                    findings.append(blob_hit)
            else:
                findings.append('unsupported_entry')

    for commit in revisions:
        raw_commit = _git(root, 'cat-file', 'commit', commit)
        commit_hit = _check_bytes(raw_commit)
        if commit_hit:
            findings.append(commit_hit)
        tree_line = raw_commit.split(b'\n', 1)[0]
        if tree_line.startswith(b'tree '):
            inspect_tree(tree_line[5:].decode().strip())
        else:
            findings.append('unsupported_entry')
    return findings


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--root', default='.')
    args = ap.parse_args()
    try:
        ok = not scan(Path(args.root))
    except Exception:
        ok = False
    print('PASS public_tree_scan' if ok else 'FAIL public_tree_scan')
    return 0 if ok else 1

if __name__ == '__main__':
    sys.exit(main())
