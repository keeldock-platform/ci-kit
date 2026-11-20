import {test} from 'node:test';
import assert from 'node:assert/strict';
import {checkCargo} from './cargo-check.mjs';
test('Cargo runs once within the unit bound and requires a passed test', () => {
 let calls=0;
 checkCargo((tool,args,options)=>{calls++;assert.equal(tool,'cargo');assert.deepEqual(args,['test','--locked','--offline']);assert.equal(options.timeout,120000);return {status:0,stdout:'1 passed',stderr:''};});
 assert.equal(calls,1);
 assert.throws(()=>checkCargo(()=>({status:0,stdout:'0 passed',stderr:''})));
});
test('Cargo preserves the compiler failure and original exit without retry', () => {
 let calls=0;
 assert.throws(()=>checkCargo(()=>{calls++;return {status:101,stdout:'',stderr:'error: compiler failure'};}),/compiler failure/);
 assert.equal(calls,1);
});
test('Cargo preserves timeout diagnostics and does not retry tests', () => {
 let calls=0;
 assert.throws(()=>checkCargo(()=>{calls++;return {status:null,error:Object.assign(new Error('spawnSync cargo ETIMEDOUT'),{code:'ETIMEDOUT'}),signal:'SIGTERM',stderr:'compiler progress'};}),/ETIMEDOUT/);
 assert.equal(calls,1);
});
