import {describe, it} from 'node:test';
import {checkCargo} from './cargo-check.mjs';
describe('rust', () => {
  it('locked unit', () => checkCargo());
});
