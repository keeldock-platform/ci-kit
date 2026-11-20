#!/bin/bash
set -euo pipefail
kit="$(pwd)/public-control"
image=mcr.microsoft.com/playwright:v1.65.1-noble@sha256:1507cbfc550a63a6b7396791ca4f00df17beb56342d2447b3f106c306f525579
base=$(mktemp -d)
cid=''
trap 'if test -n "$cid"; then docker rm -f "$cid"; fi; rm -rf "$base"' EXIT
mkdir "$base/ci"
cp "$kit/fixtures/synthetic/browser-package.json" "$base/package.json"
cp "$kit/fixtures/synthetic/browser-package-lock.json" "$base/package-lock.json"
cp "$kit/fixtures/synthetic/"{setup.mjs,setup-container.sh} "$base/ci/"
docker pull "$image"
cid=$(docker run --detach --rm --platform linux/amd64 --mount "type=bind,src=$base,dst=$base" --mount "type=bind,src=$kit,dst=/public-control,readonly" --workdir / --entrypoint /bin/sleep "$image" infinity)
docker exec --user 0 "$cid" install -d -o "$(id -u)" -g "$(id -g)" /tmp/ci-home
args=(exec --user "$(id -u):$(id -g)" --workdir "$base" --env HOME=/tmp/ci-home --env PATH=/tmp/ci-home/.cargo/bin:/tmp/ci-home/.local/bin:/usr/local/bin:/usr/bin:/bin --env CI=true --env PLAYWRIGHT_BROWSERS_PATH=/ms-playwright --env PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 "$cid")
docker "${args[@]}" /bin/sh ci/setup-container.sh web-linux
docker "${args[@]}" node -e 'const {chromium,firefox,webkit}=require("playwright");(async()=>{for(const type of [chromium,firefox,webkit]){const b=await type.launch({headless:true});const p=await b.newPage();await p.setContent("<title>public smoke</title>");if(await p.title()!=="public smoke")throw Error("title");await b.close();}})().catch(e=>{console.error(e);process.exitCode=1;});'
echo 'STAGE public-web PASS'
