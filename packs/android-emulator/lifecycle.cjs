'use strict';
// Keep lifecycle implementation in the pinned maintained modules. This adapter
// orders private byte verification before launch; upstream main continues after
// a failed pre-launch hook, which cannot enforce that ordering.
const path = require('node:path');
const fs = require('node:fs');
const exec = require('../../../public-android-tool/node_modules/@actions/exec');
const sdk = require('../../../public-android-tool/lib/sdk-installer.js');
const manager = require('../../../public-android-tool/lib/emulator-manager.js');
const input = name => process.env['INPUT_' + name];
(async () => {
  let ownsLaunch = false;
  const port = 5554;
  const stage = path.join(path.dirname(process.env.CI_PACK_STATE), 'sdk-preparation-' + input('API-LEVEL'));
  fs.mkdirSync(stage, {mode: 0o700});
  process.chdir(stage);
  try {
    await sdk.installAndroidSdk(input('API-LEVEL'), input('SYSTEM-IMAGE-API-LEVEL'), input('TARGET'), 'x86_64', 0, input('EMULATOR-BUILD'), input('NDK'), input('CMAKE'));
    await exec.exec(process.env.CI_KIT_HELPER, ['native-preflight']);
    await manager.createAvd('x86_64', input('AVD-NAME'), '2', '', false, true, '512M', input('PROFILE'), '2048M', '', input('SYSTEM-IMAGE-API-LEVEL'), input('TARGET'));
    ownsLaunch = true;
    await manager.launchEmulator(input('AVD-NAME'), true, false, true, 600, '-no-window -gpu swiftshader_indirect -no-snapshot -noaudio -no-boot-anim -accel on', false, port);
    process.chdir(input('WORKING-DIRECTORY'));
    process.env.ANDROID_SERIAL = 'emulator-5554';
    await exec.exec(process.env.CI_KIT_HELPER, ['native-hook']);
  } finally {
    if (ownsLaunch) await manager.killEmulator(port);
  }
})().catch(() => { process.exitCode = 1; });
