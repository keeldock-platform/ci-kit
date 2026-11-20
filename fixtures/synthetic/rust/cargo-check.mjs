import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';

// The enclosing unit owns the 300-second deadline. Run Cargo once, retaining
// its original failure and bounded streams in the private JUnit assertion.
export function checkCargo(run = spawnSync) {
  const result = run('cargo', ['test', '--locked', '--offline'], {
    encoding: 'utf8', timeout: 120000, maxBuffer: 4 * 1024 * 1024,
  });
  const detail = [result.error?.message, result.signal,
    result.stdout?.slice(-65536), result.stderr?.slice(-65536)].filter(Boolean).join('\n');
  assert.equal(result.error, undefined, detail);
  assert.equal(result.status, 0, detail);
  assert.match(result.stdout, /1 passed/);
}
