'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
let plugin;
try { plugin = require('./adapter.cjs'); } catch (error) {
  if (error.code !== 'MODULE_NOT_FOUND') throw error;
}
test('projects and product dependencies use the public plugin contract', { skip: !plugin && 'reviewed dependencies are not provisioned' }, async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'ci-plugin-test-'));
  try {
    const features = [
      { name: 'plugin-alpha', root: 'alpha', inputs: ['alpha/**', 'shared/api.json'], checks: ['unit', 'lint'] },
      { name: 'plugin-beta', root: 'beta', inputs: ['beta/**'], checks: ['unit'] },
    ];
    fs.writeFileSync(path.join(root, 'catalogue.json'), JSON.stringify({ features, edges: [['plugin-beta', 'plugin-alpha']] }));
    const context = { workspaceRoot: root };
    const results = await plugin.createNodes[1](['catalogue.json'], {}, context);
    assert.equal(results[0][0], 'catalogue.json');
    const nodes = results[0][1];
    assert.equal(nodes.projects.alpha.name, 'plugin-alpha');
    assert.deepEqual(nodes.projects.alpha.namedInputs.default, ['{workspaceRoot}/alpha/**', '{workspaceRoot}/shared/api.json']);
    assert.deepEqual(Object.keys(nodes.projects.alpha.targets).sort(), ['lint', 'unit']);
    const edges = plugin.createDependencies({}, context);
    assert.equal(edges[0].source, 'plugin-beta');
    assert.equal(edges[0].target, 'plugin-alpha');
    assert.equal(edges[0].sourceFile, 'beta/ci-unit.json');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
