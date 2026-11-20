'use strict';
const required=['platform-tools','emulator','build-tools;37.0.0','ndk;30.0.16248370','cmake;4.1.2','platforms;android-35','platforms;android-36','system-images;android-35;google_apis;x86_64','system-images;android-36;google_apis;x86_64'];
function installedPackages(table){
 const packages={};
 for(const line of table.split('\n')){
  const cols=line.split('|').map(s=>s.trim());const key=cols[0],version=cols[1];
  if((required.includes(key)||/^cmdline-tools;(latest|[0-9]+(?:\.[0-9]+){0,3})$/.test(key))&&/^[0-9]+(?:\.[0-9]+){0,3}$/.test(version||'')){
   if(Object.hasOwn(packages,key))throw new Error('duplicate installed package');packages[key]=version;
  }
 }
 const commandLine=Object.keys(packages).filter(key=>key.startsWith('cmdline-tools;'));
 const missing=required.filter(key=>!packages[key]);
 if(commandLine.length!==1)missing.push('cmdline-tools');
 return {packages,missing,complete:missing.length===0};
}
module.exports={installedPackages};
