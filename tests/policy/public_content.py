#!/usr/bin/env python3
"""Fail-closed publication scan; the denylist is supplied outside the public tree."""
import argparse
import base64
import hashlib
import html
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import unicodedata
from urllib.parse import unquote

MAX_FILE = 16 * 1024 * 1024
MAX_COMMITS = 1000


def git(root, *args):
    p = subprocess.run(['git', '--no-replace-objects', '-C', str(root), *args], capture_output=True, check=False)
    if p.returncode:
        raise ValueError('git_unavailable')
    return p.stdout


def pairs(items):
    d = {}
    for key, value in items:
        if key in d:
            raise ValueError('invalid_policy')
        d[key] = value
    return d


def norm(text):
    return unicodedata.normalize('NFKC', text).casefold()


def views(data):
    if len(data) > MAX_FILE:
        raise ValueError('size_limit')
    if data.startswith((b'\xff\xfe', b'\xfe\xff')):
        text = data.decode('utf-16', errors='strict')
    else:
        text = data.decode('utf-8-sig', errors='strict')
    if '\x00' in text:
        raise ValueError('uninspectable_content')
    result = {norm(text)}
    # Bounded decoding covers JSON escapes, HTML/URL encoding and base64 tokens.
    for _ in range(3):
        decoded = set()
        for s in result:
            decoded.add(norm(html.unescape(unquote(s))))
            decoded.add(norm(re.sub(r'\\u([0-9a-fA-F]{4})', lambda m: chr(int(m[1], 16)), s)))
            decoded.add(norm(re.sub(r'\\x([0-9a-fA-F]{2})', lambda m: chr(int(m[1], 16)), s)))
        result |= decoded
    for token in re.findall(rb'[A-Za-z0-9+/_-]{8,}={0,2}', data):
        try:
            decoded = base64.b64decode(token + b'=' * (-len(token) % 4), altchars=b'-_', validate=True).decode('utf-8')
            result.add(norm(decoded))
        except (ValueError, UnicodeError):
            pass
    return result


def load_policy(path, root):
    path = path.resolve(strict=True)
    if path.is_relative_to(root):
        raise ValueError('policy_must_be_private')
    data = path.read_bytes()
    if len(data) > 1024 * 1024:
        raise ValueError('invalid_policy')
    p = json.loads(data.decode('utf-8'), object_pairs_hook=pairs)
    if set(p) != {'schema_version', 'terms', 'markers'} or p['schema_version'] != 1:
        raise ValueError('invalid_policy')
    if not all(isinstance(p[k], list) and p[k] for k in ('terms', 'markers')):
        raise ValueError('invalid_policy')
    entries = p['terms'] + p['markers']
    if not all(isinstance(s, str) and 3 <= len(s) <= 1024 for s in entries):
        raise ValueError('invalid_policy')
    return [norm(s) for s in entries]


def scan(root, policy, base=None, head='HEAD', ref=None, all_refs=False):
    root = root.resolve(strict=True)
    terms = load_policy(policy, root)
    hits = []
    seen = set()
    classified = {}
    blobs = {}
    def classify(data):
        digest = hashlib.sha256(data).digest()
        if digest not in classified:
            try:
                texts = views(data)
                found = [i for i, term in enumerate(terms) if any(term in s for s in texts)]
                classified[digest] = {'reason': 'denylist_match', 'term_indices': found} if found else None
            except (ValueError, UnicodeError):
                classified[digest] = {'reason': 'uninspectable_content'}
        return classified[digest]
    def record(label, result):
        if result is not None:
            hits.append({'location': label, **result})
    def examine(label, data):
        key = (label, hashlib.sha256(data).digest())
        if key not in seen:
            seen.add(key)
            record(label, classify(data))
    def examine_blob(label, oid):
        # An immutable object is inspected once, while every path/revision keeps
        # its own finding. Cache verdicts only, never private object contents.
        if oid not in blobs:
            size = int(git(root, 'cat-file', '-s', oid))
            if size > MAX_FILE:
                blobs[oid] = {'reason': 'size_limit'}
            else:
                blobs[oid] = classify(git(root, 'cat-file', 'blob', oid))
        record(label, blobs[oid])
    if ref:
        examine('ref', ref.encode())
    # Scan both index blobs and current files: a clean index cannot conceal local edits.
    for stage in git(root, 'ls-files', '--stage', '-z', '--cached').split(b'\0'):
        if not stage:
            continue
        metadata, raw = stage.split(b'\t', 1)
        name = os.fsdecode(raw)
        examine('path:' + name, raw)
        meta = metadata.split()
        if len(meta) != 3 or meta[2] != b'0' or meta[0] == b'160000':
            hits.append({'location': name, 'reason': 'uninspectable_index'})
            continue
        examine_blob('index:' + name, meta[1].decode())
        path = root / name
        if path.is_symlink():
            examine('symlink:' + name, os.fsencode(os.readlink(path)))
        elif path.is_file():
            if path.stat().st_size > MAX_FILE:
                hits.append({'location': name, 'reason': 'size_limit'})
            else:
                examine('worktree:' + name, path.read_bytes())
        else:
            hits.append({'location': name, 'reason': 'missing_file'})
    if not re.fullmatch(r'[a-f0-9]{40}', git(root, 'rev-parse', '--verify', head).decode().strip()):
        raise ValueError('invalid_revision')
    revisions = git(root, 'rev-list', '--max-count=' + str(MAX_COMMITS + 1), head).decode().splitlines()
    if all_refs:
        revisions = git(root, 'rev-list', '--max-count=' + str(MAX_COMMITS + 1), '--all').decode().splitlines()
        for row in git(root, 'for-each-ref', '--format=%(refname) %(objecttype) %(objectname)').decode().splitlines():
            name, kind, oid = row.split(' ')
            examine('publication-ref', name.encode())
            if kind == 'tag': examine('publication-tag:' + oid, git(root, 'cat-file', 'tag', oid))
    if len(revisions) > MAX_COMMITS: raise ValueError('commit_limit')
    if base:
        if not re.fullmatch(r'[a-f0-9]{40}', base):
            raise ValueError('invalid_revision')
        git(root, 'merge-base', '--is-ancestor', base, head)
        revisions = git(root, 'rev-list', '--max-count=' + str(MAX_COMMITS + 1), base + '..' + head).decode().splitlines()
        if len(revisions) > MAX_COMMITS:
            raise ValueError('commit_limit')
    revisions = list(dict.fromkeys([git(root, 'rev-parse', '--verify', head).decode().strip(), *revisions]))
    for commit in revisions:
        examine('commit:' + commit, git(root, 'cat-file', 'commit', commit))
        for row in git(root, 'ls-tree', '-rz', '--full-tree', commit).split(b'\0'):
            if not row:
                continue
            meta, name = row.split(b'\t', 1)
            mode, kind, oid = meta.split()
            examine('history-path:' + commit, name)
            if kind != b'blob':
                hits.append({'location': commit, 'reason': 'uninspectable_tree'})
                continue
            examine_blob('history:' + commit + ':' + os.fsdecode(name), oid.decode())
    return hits


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--root', default='.')
    ap.add_argument('--policy', required=True)
    ap.add_argument('--base')
    ap.add_argument('--head', default='HEAD')
    ap.add_argument('--ref')
    ap.add_argument('--private-report')
    ap.add_argument('--all-refs', action='store_true')
    args = ap.parse_args()
    try:
        root = Path(args.root).resolve(strict=True)
        hits = scan(root, Path(args.policy), args.base, args.head, args.ref, args.all_refs)
        if args.private_report:
            out = Path(args.private_report).resolve()
            if out.is_relative_to(root):
                raise ValueError('report_must_be_private')
            fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
            os.fchmod(fd, 0o600)
            with os.fdopen(fd, 'w') as f:
                json.dump({'schema_version': 1, 'ok': not hits, 'findings': hits}, f, indent=2)
        print('FAIL public_content' if hits else 'PASS public_content')
        return int(bool(hits))
    except Exception:
        print('FAIL public_content_policy')
        return 1

if __name__ == '__main__':
    sys.exit(main())
