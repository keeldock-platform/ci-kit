// Source-free hosted tool inventory. No environment dump or private checkout.
import {spawnSync} from 'node:child_process';
import fs from 'node:fs';
import crypto from 'node:crypto';
import path from 'node:path';
const result={platform:process.platform,arch:process.arch,node:process.version};
function run(argv){const p=spawnSync(argv[0],argv.slice(1),{encoding:'utf8',timeout:120000,maxBuffer:4*1024*1024});return {code:p.status,stdout:p.stdout||'',stderr:p.stderr||''};}
if(process.platform==='darwin'){
 result.xcode=run(['xcodebuild','-version']);
 result.developer=run(['xcode-select','-p']);
 result.sdk=run(['xcrun','--sdk','iphonesimulator','--show-sdk-version']);
 result.runtimes=run(['xcrun','simctl','list','runtimes','-j']);
 result.deviceTypes=run(['xcrun','simctl','list','devicetypes','-j']);
}
if(process.platform==='linux'){
 result.docker=run(['docker','version','--format','{{.Server.Version}}']);
 const sdk=process.env.ANDROID_HOME||process.env.ANDROID_SDK_ROOT;
 if(sdk){
  result.android={packages:{},tools:{}};
  function walk(dir,depth=0){if(depth>5||!fs.existsSync(dir))return;for(const e of fs.readdirSync(dir,{withFileTypes:true})){const file=path.join(dir,e.name);if(e.isDirectory())walk(file,depth+1);if(e.name==='source.properties'){const text=fs.readFileSync(file,'utf8');result.android.packages[path.relative(sdk,dir)]=Object.fromEntries(text.split('\n').filter(s=>/^Pkg\.(Revision|Path)/.test(s)).map(s=>s.split(/\s*=\s*/,2)));}}}
  walk(sdk);
  for(const rel of ['platform-tools/adb','emulator/emulator','cmdline-tools/latest/bin/sdkmanager','cmdline-tools/latest/bin/avdmanager']){const file=path.join(sdk,rel);if(fs.existsSync(file))result.android.tools[rel]=crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');}
 }
}
console.log('PUBLIC_TOOL_INVENTORY '+JSON.stringify(result));
