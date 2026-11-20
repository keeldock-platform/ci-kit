import {describe,it} from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import path from 'node:path';
import {createRequire} from 'node:module';
const local = createRequire(path.resolve('package.json'));
describe('venue',()=>{
 it('readiness',async()=>{
  const platform=process.env.FIXTURE_PLATFORM, variant=process.env.FIXTURE_VARIANT;
  if(platform==='ios-simulator'){
   const udid=process.env.KD_CI_SIMULATOR_UDID;
   assert.match(udid||'',/^[a-f0-9-]{36}$/i);
   const model=execFileSync('/usr/bin/xcrun',['simctl','getenv',udid,'SIMULATOR_MODEL_IDENTIFIER'],{encoding:'utf8',timeout:30000}).trim();
   assert.ok(model.startsWith(variant==='phone'?'iPhone':'iPad'));
  }else if(platform==='android-emulator'){
   const sdk=process.env.ANDROID_HOME||process.env.ANDROID_SDK_ROOT;
   const serial=process.env.ANDROID_SERIAL;
   assert.equal(serial,'emulator-5554');
   const api=execFileSync(path.join(sdk,'platform-tools/adb'),['-s',serial,'shell','getprop','ro.build.version.sdk'],{encoding:'utf8',timeout:30000}).trim();
   assert.equal(api,variant.replace('api',''));
  }else if(platform==='web-linux'){
   assert.ok(['chromium','firefox','webkit'].includes(variant));
   assert.equal(local('playwright/package.json').version,'1.65.1');
   const browser=await local('playwright')[variant].launch({headless:true});
   try{const page=await browser.newPage();await page.setContent('<main>fixture-ready</main>');assert.equal(await page.textContent('main'),'fixture-ready');}finally{await browser.close();}
  }else assert.fail('invalid venue');
 });
});
