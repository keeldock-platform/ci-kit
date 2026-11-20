import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
for (const name of ['rust','typescript','plugin-alpha','plugin-beta','native-ios-simulator','native-android-emulator','native-web-linux']) fs.mkdirSync(`${name}/reports`,{recursive:true,mode:0o700});
if(process.argv[2]==='web-linux') {
 const result=spawnSync('npm',['ci','--ignore-scripts','--no-audit','--no-fund'],{stdio:'inherit',timeout:120000});
 if(result.status!==0)process.exitCode=1;
}
