"""Closed public diagnostic set: only fixed public-control bootstrap commands."""

def source_free_bootstrap(text):
    try:
        public=text[text.index('    - name: Public control'):]
        steps=[line.split('name: ',1)[1] for line in public.splitlines() if line.startswith('    - name: ')]
        if steps!=['Public control','Go','Node','Build helper']:return False
        repositories=[line.strip() for line in public.splitlines() if line.strip().startswith('repository:')]
        if repositories!=['repository: keeldock-platform/ci-kit']:return False
        script=public.split('      run: |\n',1)[1]
        commands=[line.strip() for line in script.splitlines() if line.strip()]
        if len(commands)!=7:return False
        if commands[0]!='set -euo pipefail':return False
        expected_trap = r'''trap 'code=$?; printf "PUBLIC_TOOL_FAILURE exit=%s os=%s arch=%s\n" "$code" "$RUNNER_OS" "$RUNNER_ARCH"; git --version; go version; node --version; exit "$code"' ERR'''
        if commands[1]!=expected_trap:return False
        if commands[2:]!=[
            'test "$(git -C public-control rev-parse HEAD)" = "$CI_KIT_SHA"',
            'cd public-control',
            'npm ci --prefix planner --ignore-scripts --no-audit --no-fund',
            'go mod verify',
            'go build -trimpath -mod=readonly -o "$RUNNER_TEMP/ci-kit" ./cmd/ci-kit',
        ]:return False
        return not any(value in public for value in ('SOURCE_READ','SOURCE_MAP','CI_KIT_OPERATION','ci-private-'))
    except (ValueError,IndexError):return False
