'use strict';
// Source-free public SDK inventory for a later signed private support lock.
const path = require('node:path');
const fs = require('node:fs');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');
const sdk = require('../../../public-android-tool/lib/sdk-installer.js');
(async () => {
 const sdkRoot = process.env.ANDROID_HOME;
 if (!path.isAbsolute(sdkRoot || '')) throw new Error('SDK unavailable');
 const stage = path.join(process.env.RUNNER_TEMP, 'source-free-sdk');
 fs.mkdirSync(stage, {recursive:true});process.chdir(stage);
 for (const api of ['35','36']) await sdk.installAndroidSdk(api, api, 'google_apis', 'x86_64', 0, '16916650', '30.0.16248370', '4.1.2');
 const table = spawnSync(path.join(sdkRoot,'cmdline-tools/latest/bin/sdkmanager'), ['--list_installed'], {encoding:'utf8',timeout:120000,maxBuffer:1048576});
 if (table.status !== 0) throw new Error('SDK inventory failed');
 const packages = {};
 const wanted = ['platform-tools','emulator','build-tools;37.0.0','cmdline-tools;latest','ndk;30.0.16248370','cmake;4.1.2','platforms;android-35','platforms;android-36','system-images;android-35;google_apis;x86_64','system-images;android-36;google_apis;x86_64'];
 for (const line of table.stdout.split('\n')) {const cols=line.split('|').map(s=>s.trim());if(wanted.includes(cols[0]) && /^[0-9]+(\.[0-9]+){0,3}$/.test(cols[1]))packages[cols[0]]=cols[1];}
 if (Object.keys(packages).length !== wanted.length) throw new Error('SDK packages incomplete');
 const tools = {};
 for (const rel of ['platform-tools/adb','emulator/emulator','cmdline-tools/latest/bin/sdkmanager','cmdline-tools/latest/bin/avdmanager','system-images/android-35/google_apis/x86_64/system.img','system-images/android-36/google_apis/x86_64/system.img']) {
  const hash=crypto.createHash('sha256');for await (const bytes of fs.createReadStream(path.join(sdkRoot,rel)))hash.update(bytes);tools[rel]=hash.digest('hex');
 }
 const support={schema_version:1,remaining_working_set_bytes:1073741824,android:{emulator_build:'16916650',ndk:'30.0.16248370',cmake:'4.1.2',images:[{variant:'api35',api:35,target:'google_apis',profile:'pixel_6'},{variant:'api36',api:36,target:'google_apis',profile:'pixel_6'}],packages,tool_sha256:tools}};
 console.log('PUBLIC_ANDROID_SUPPORT '+JSON.stringify(support));
})().catch(() => {console.log('STAGE public-sdk FAIL');process.exitCode=1;});
