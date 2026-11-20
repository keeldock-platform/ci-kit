#!/usr/bin/env python3
"""Fail closed on drift in an external, owner-only deployment handoff."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys

PIN = re.compile(r'[\w.-]+/[\w./-]+@[0-9a-f]{40}')
KIT = 'keeldock-platform/ci-kit/'
ACTIVE_KEYS = {'production_sha', 'reviewed_production_sha', 'kit_sha',
               'configuration_kit_sha'}


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def execution_files(root):
    return sorted(p for p in (root / '.github').rglob('*') if p.is_file() and
                  (p.parent == root / '.github/workflows' and p.suffix in {'.yml', '.yaml'} or
                   p.name in {'action.yml', 'action.yaml'}))


def remote_uses(root, sha, caller=False):
    """Scan workflows AND composites, including currently dormant execution paths.

    This follows the repository's block-style YAML policy. Unknown/flow-style
    uses syntax fails closed instead of silently losing a dependency.
    """
    refs = set()
    files = execution_files(root)
    require(files, 'missing execution files')
    for path in files:
        for line in path.read_text().splitlines():
            if line.lstrip().startswith('#'):
                continue
            if re.search(r'\buses\s*:', line):
                match = re.fullmatch(r'\s*(?:-\s*)?uses:\s*[\'\"]?([^\s\'\"#]+)[\'\"]?\s*(?:#.*)?', line)
                require(match is not None, 'unsupported uses syntax')
                ref = match[1]
                if ref.startswith(('$/', './')):
                    require(not caller, 'caller local execution bypass')
                    local = root / ref[2:]
                    require(local.resolve().is_relative_to(root.resolve()), 'local execution escape')
                    require(local.is_file() or any((local / n).is_file() for n in ('action.yml', 'action.yaml')),
                            'missing local execution target')
                    continue
                require(PIN.fullmatch(ref), 'mutable execution pin')
                if caller or ref.startswith(KIT):
                    require(ref.startswith(KIT) and ref.endswith('@' + sha), 'caller execution pin differs from HEAD')
                refs.add(ref)
            match = re.match(r'\s+kit[-_]sha:\s*(.*?)\s*(?:#.*)?$', line)
            if match and caller:
                require(match[1].strip('\'\"') == sha, 'caller input pin differs from HEAD')
    require(refs, 'missing remote execution pins')
    return refs


def check(root, settings, callers):
    root, settings, callers = root.resolve(), settings.resolve(), callers.resolve()
    sha = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
    require(re.fullmatch('[0-9a-f]{40}', sha), 'invalid production HEAD')
    require(not subprocess.check_output(['git', '-C', str(root), 'status', '--porcelain'], text=True).strip(),
            'production tree is dirty')
    doc = json.loads(settings.read_text())
    require(doc['production_sha'] == sha and doc['reconciliation']['reviewed_production_sha'] == sha,
            'settings production pin differs from HEAD')

    def active_pins(value):
        if isinstance(value, dict):
            for key, item in value.items():
                if key in ACTIVE_KEYS:
                    require(item == sha, 'settings active pin differs from HEAD')
                active_pins(item)
        elif isinstance(value, list):
            for item in value:
                active_pins(item)
    active_pins(doc)
    rows = doc['settings']
    require(len({r['id'] for r in rows}) == len(rows) == doc['settings_count'], 'settings count mismatch')
    orgs = {r['target'].lower(): r for r in rows if r['kind'] == 'organization_actions_permissions'}
    repos = {r['target'].lower(): r for r in rows if r['kind'] == 'repository_actions_permissions'}
    contracts = [r for r in rows if r['kind'] == 'caller_workflow_contract']
    require(orgs and repos and contracts, 'missing policy or caller contract')
    dependencies = remote_uses(root, sha)
    candidate = {KIT + '.github/workflows/' + n + '.yml@' + sha for n in ('ci-policy', 'validate')}
    expected_dirs = set()
    for row in contracts:
        values = row['values']
        require(values['kit_sha'] == sha, 'missing caller kit pin')
        template = (settings.parent / values['templates']).resolve()
        require(template.parent == callers and template.is_dir(), 'caller template path mismatch')
        expected_dirs.add(template)
        require(not subprocess.check_output(['git', '-C', str(template), 'status', '--porcelain'], text=True).strip(),
                'caller tree is dirty')
        candidate |= remote_uses(template, sha, caller=True)
        # Lock provenance can contain a second pin independently of workflow YAML.
        lock = template / '.github/workflows/actions.lock'
        require(lock.is_file(), 'missing caller lock')
        for ref in re.findall(r'keeldock-platform/ci-kit/[\w./-]+@[\w.-]+', lock.read_text()):
            require(ref.endswith('@' + sha), 'caller lock pin differs from HEAD')
        require(row['target'].lower() in repos, 'missing caller child policy')
    require(expected_dirs == {p.resolve() for p in callers.iterdir() if p.is_dir()}, 'orphan or missing caller')
    required = dependencies | candidate
    for row in [*orgs.values(), *repos.values()]:
        values = row['values']
        pins = set(values['patterns_allowed'])
        require(values['allowed_actions'] == 'selected' and values['sha_pinning_required'] is True and
                values['github_owned_allowed'] is False and values['verified_allowed'] is False,
                'broad action access')
        require(all(PIN.fullmatch(pin) for pin in pins), 'mutable allowlist pin')
        require(required <= pins, 'used action missing from allowlist: ' + row['id'])
        if row['kind'] == 'repository_actions_permissions':
            parent = orgs[row['target'].split('/')[0].lower()]
            require(pins <= set(parent['values']['patterns_allowed']), 'child pin missing from parent allowlist')
    # Old exact allowlist refs are coexistence evidence, never active candidate pins.
    inventory = json.loads((settings.parent / doc['reconciliation']['inventory_file']).read_text())
    for repo in inventory['repositories']:
        name = repo['repository'].lower()
        used = {c['uses'] for c in repo['consumers'] if not c['uses'].startswith(('./', '$/'))}
        require(all(PIN.fullmatch(ref) for ref in used), 'mutable inventoried action')
        require(used <= set(orgs[name.split('/')[0]]['values']['patterns_allowed']), 'legacy action missing from parent')
        if name in repos:
            require(used <= set(repos[name]['values']['patterns_allowed']), 'legacy action missing from child')
    return {'production_sha': sha, 'pin_consistency': 'PASS', 'allowlist_complete': True,
            'callers': len(contracts), 'policy_lists': len(orgs) + len(repos),
            'required_execution_refs': sorted(required)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path('.'))
    parser.add_argument('--settings', type=Path, required=True)
    parser.add_argument('--callers', type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(check(args.root, args.settings, args.callers), sort_keys=True))
        return 0
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        print('FAIL handoff_consistency: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
