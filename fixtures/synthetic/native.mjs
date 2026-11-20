import {spawnSync} from 'node:child_process';
const [platform,variant] = process.argv.slice(2);
if (!['ios-simulator','android-emulator','web-linux'].includes(platform) || !/^[a-z0-9-]+$/.test(variant || '')) throw new Error('invalid venue');
const result = spawnSync(process.execPath, ['--test', '--test-reporter=junit', `--test-reporter-destination=native-${platform}/reports/unit.xml`, 'ci/native.test.mjs'], {stdio:'inherit',env:{...process.env,FIXTURE_PLATFORM:platform,FIXTURE_VARIANT:variant},timeout:120000});
process.exitCode = result.status === 0 ? 0 : 1;
