import {test} from 'node:test';
import assert from 'node:assert/strict';
import {simulatorModel} from './native-simctl-query.mjs';
test('one infrastructure timeout retries the identical bounded query',()=>{
 let calls=0;
 const model=simulatorModel('owned-device',(tool,args,options)=>{
  assert.equal(tool,'/usr/bin/xcrun');assert.deepEqual(args,['simctl','getenv','owned-device','SIMULATOR_MODEL_IDENTIFIER']);assert.equal(options.timeout,60000);
  if(++calls===1)throw Object.assign(new Error('timeout'),{code:'ETIMEDOUT'});
  return 'iPad17,1\n';
 });assert.equal(model,'iPad17,1');assert.equal(calls,2);
});
test('persistent timeout stops at two queries',()=>{
 let calls=0;assert.throws(()=>simulatorModel('owned-device',()=>{calls++;throw Object.assign(new Error('timeout'),{code:'ETIMEDOUT'});}),/timeout/);assert.equal(calls,2);
});
test('command failure is not retried and wrong device is returned for assertion',()=>{
 let calls=0;assert.throws(()=>simulatorModel('owned-device',()=>{calls++;throw new Error('native failure');}),/native failure/);assert.equal(calls,1);
 calls=0;assert.equal(simulatorModel('owned-device',()=>{calls++;return 'wrong-device';}),'wrong-device');assert.equal(calls,1);
});
