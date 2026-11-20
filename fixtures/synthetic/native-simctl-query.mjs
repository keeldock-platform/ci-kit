import {execFileSync} from 'node:child_process';
// Prior hosted query exceeded 30s. Allow 60s per query, at most two attempts;
// the 180s native wrapper and 300s unit bound contain the complete operation.
export function simulatorModel(udid, run=execFileSync, measure=()=>performance.now()) {
 const start=measure();
 for(let attempt=1;attempt<=2;attempt++) {
  try {
   const model=run('/usr/bin/xcrun',['simctl','getenv',udid,'SIMULATOR_MODEL_IDENTIFIER'],{encoding:'utf8',timeout:60000}).trim();
   console.log(`SIMULATOR_QUERY attempts=${attempt} milliseconds=${Math.round(measure()-start)}`);
   return model;
  } catch(error) {
   if(error.code!=='ETIMEDOUT'||attempt===2) throw error;
  }
 }
}
