'use strict';
const {test}=require('node:test');const assert=require('node:assert/strict');const {installedPackages}=require('./android-inventory.cjs');
const rows=['platform-tools','emulator','build-tools;37.0.0','ndk;30.0.16248370','cmake;4.1.2','platforms;android-35','platforms;android-36','system-images;android-35;google_apis;x86_64','system-images;android-36;google_apis;x86_64'].map(key=>`${key} | 1.0 | public package`);
test('versioned SDK command line package is retained',()=>{const data=installedPackages([...rows,'cmdline-tools;12.0 | 12.0 | SDK'].join('\n'));assert.equal(data.complete,true);assert.equal(data.packages['cmdline-tools;12.0'],'12.0');assert.equal(Object.hasOwn(data.packages,'cmdline-tools;latest'),false);});
test('missing SDK component remains incomplete',()=>{const data=installedPackages(rows.join('\n'));assert.equal(data.complete,false);assert.deepEqual(data.missing,['cmdline-tools']);});
test('duplicate package is refused',()=>assert.throws(()=>installedPackages([...rows,rows[0]].join('\n'))));
