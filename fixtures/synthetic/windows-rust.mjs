// Source-free native Rust preparation. Only pinned public tools run here.
import {spawnSync} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
assert.equal(process.platform, 'win32');
const host = {x64:'x86_64-pc-windows-msvc',arm64:'aarch64-pc-windows-msvc'}[process.arch];
assert.ok(host, 'unsupported native Rust host');
const version='1.93.0';
const toolchain=version+'-'+host;
function run(tool,args,timeout=30000) {
 const result=spawnSync(tool,args,{encoding:'utf8',timeout,maxBuffer:4*1024*1024});
 if(result.stdout) process.stdout.write(result.stdout);
 if(result.stderr) process.stderr.write(result.stderr);
 assert.ifError(result.error);assert.equal(result.status,0,'public native Rust command failed');
 return result.stdout.trim();
}
run('rustup',['--version']);
run('rustup',['toolchain','install',toolchain,'--profile','minimal','--no-self-update'],240000);
const cargo=run('rustup',['which','--toolchain',toolchain,'cargo']);
const rustc=run('rustup',['which','--toolchain',toolchain,'rustc']);
for(const tool of [cargo,rustc]) {
 assert.ok(path.isAbsolute(tool)&&!/[\r\n]/.test(tool));
 assert.ok(fs.statSync(tool).isFile()&&!fs.lstatSync(tool).isSymbolicLink());
}
assert.equal(path.dirname(cargo),path.dirname(rustc));
const inventory=run(rustc,['-vV']);
assert.ok(inventory.split(/\r?\n/).includes('host: '+host));
assert.ok(inventory.split(/\r?\n/).includes('release: '+version));
run(cargo,['--version']);
// Direct pinned binaries avoid implicit rustup proxy setup in private commands.
fs.appendFileSync(process.env.GITHUB_PATH,path.dirname(cargo)+'\n');
console.log('STAGE native-rust-preparation PASS');
