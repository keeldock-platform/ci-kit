import {describe, it} from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
describe('rust', () => {
  it('locked unit', () => {
    const result = spawnSync('cargo', ['test', '--locked', '--offline'], {encoding: 'utf8', timeout: 60000});
    assert.equal(result.status, 0);
    assert.match(result.stdout, /1 passed/);
  });
});
