import {execFileSync} from 'node:child_process';
// Starting CoreSimulator once avoids charging service startup to a device query.
const start=performance.now();
execFileSync('/usr/bin/xcrun',['simctl','list','devices','--json'],{timeout:90000,stdio:['ignore','ignore','pipe'],env:{...process.env,DEVELOPER_DIR:'/Applications/Xcode_26.6.app/Contents/Developer'}});
console.log(`PUBLIC_SIMULATOR_SERVICE ready milliseconds=${Math.round(performance.now()-start)}`);
