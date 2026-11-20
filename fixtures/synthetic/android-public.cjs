'use strict';
// Reader-free reproduction of the same pinned API36 emulator settings.
const path = require('node:path');
const fs = require('node:fs');
const net = require('node:net');
const {execFileSync} = require('node:child_process');
const manager = require('../../../public-android-tool/lib/emulator-manager.js');
(async () => {
  const sdk = process.env.ANDROID_HOME;
  if (!path.isAbsolute(sdk || '') || !path.isAbsolute(process.env.HOME || '')) throw Error('public SDK root');
  process.env.ANDROID_AVD_HOME = path.join(process.env.HOME, '.android/avd');
  process.env.PATH = [path.join(sdk, 'cmdline-tools/latest/bin'), path.join(sdk, 'platform-tools'), process.env.PATH].join(path.delimiter);
  const name = 'public-api36-probe';
  if (fs.existsSync(path.join(process.env.ANDROID_AVD_HOME, name + '.avd')) || fs.existsSync(path.join(process.env.ANDROID_AVD_HOME, name + '.ini'))) throw Error('existing public AVD');
  await new Promise((resolve, reject) => {
    const socket = net.connect({host:'127.0.0.1', port:5554});
    socket.setTimeout(1000, () => {socket.destroy(); reject(Error('public port timeout'));});
    socket.on('connect', () => {socket.destroy(); reject(Error('existing emulator'));});
    socket.on('error', error => error.code === 'ECONNREFUSED' ? resolve() : reject(error));
  });
  let created = false, launched = false;
  try {
    created = true;
    await manager.createAvd('x86_64', name, '2', '', false, true, '512M', 'pixel_6', '2048M', '', '36', 'google_apis');
    launched = true;
    await manager.launchEmulator(name, true, false, true, 600, '-no-window -gpu swiftshader_indirect -no-snapshot -noaudio -no-boot-anim -accel on', false, 5554);
  } finally {
    if (launched) {
      await manager.killEmulator(5554);
      execFileSync(path.join(sdk, 'platform-tools/adb'), ['-s', 'emulator-5554', 'wait-for-disconnect'], {stdio:'inherit', timeout:30000});
    }
    if (created) execFileSync(path.join(sdk, 'cmdline-tools/latest/bin/avdmanager'), ['delete', 'avd', '-n', name], {stdio:'inherit', timeout:30000});
  }
  console.log('STAGE public-android PASS');
})().catch(error => {console.error(error); process.exitCode = 1;});
