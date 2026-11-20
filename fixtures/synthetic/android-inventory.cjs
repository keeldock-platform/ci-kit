'use strict';
const required=['platform-tools','emulator','build-tools;37.0.0','ndk;30.0.16248370','cmake;4.1.2','platforms;android-35','platforms;android-36','system-images;android-35;google_apis;x86_64','system-images;android-36;google_apis;x86_64'];
function installedPackages(table,properties){
 const packages={};
 for(const line of table.split('\n')){
  const cols=line.split('|').map(s=>s.trim());const key=cols[0],version=cols[1];
  if((required.includes(key)||/^cmdline-tools;(latest|[0-9]+(?:\.[0-9]+){0,3})$/.test(key))&&/^[0-9]+(?:\.[0-9]+){0,3}$/.test(version||'')){
   if(Object.hasOwn(packages,key))throw new Error('duplicate installed package');packages[key]=version;
  }
 }
 const revisionLines=(properties||'').split('\n').filter(line=>/^\s*Pkg\.Revision\s*=/.test(line));
 const commandLineVersion=revisionLines.length===1?revisionLines[0].slice(revisionLines[0].indexOf('=')+1).trim():'';
 const commandLine=Object.keys(packages).filter(key=>key.startsWith('cmdline-tools;'));
 const missing=required.filter(key=>!packages[key]);
 if(!/^[0-9]+(?:\.[0-9]+){1,3}$/.test(commandLineVersion)||commandLine.length>1||commandLine.some(key=>packages[key]!==commandLineVersion))missing.push('cmdline-tools');
 return {packages,commandLineVersion,missing,complete:missing.length===0};
}
module.exports={installedPackages};
