import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { sum } from './sum.ts';
describe('arithmetic', () => {
  it('adds whole numbers', () => assert.equal(sum(2, 3), 5));
});
