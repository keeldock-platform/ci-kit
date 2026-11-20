import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// Produce a disposable generic source tree for the normal catalogue/CLI entry.
const target = process.argv[2];
if (!target || !path.isAbsolute(target) || fs.existsSync(target)) throw new Error('a new absolute output directory is required');
fs.mkdirSync(target, { mode: 0o700 });
const fixtureRoot = path.dirname(fileURLToPath(import.meta.url));
fs.mkdirSync(path.join(target, 'ci'), { mode: 0o700 });
const platforms = ['linux-debian', 'linux-arch', 'macos-arm64', 'macos-x64', 'windows-x64', 'windows-arm64', 'ios-simulator', 'android-emulator', 'web-linux', 'server-linux-x64', 'server-linux-arm64'];
const hostPlatforms = platforms.filter(p => !['ios-simulator','android-emulator','web-linux'].includes(p));
const nativePlatforms = ['ios-simulator','android-emulator','web-linux'];
const scenario = process.argv[3] || 'success';
if (!['success','failure','stdout','stderr','ansi','annotation','crash','timeout','cancel'].includes(scenario)) throw new Error('invalid scenario');
const support = process.env.CI_SYNTHETIC_SUPPORT_FILE ? JSON.parse(fs.readFileSync(process.env.CI_SYNTHETIC_SUPPORT_FILE, 'utf8')) : {};
fs.copyFileSync(path.join(fixtureRoot, 'browser-package.json'), path.join(target, 'package.json'));
fs.copyFileSync(path.join(fixtureRoot, 'browser-package-lock.json'), path.join(target, 'package-lock.json'));
for (const file of ['native.mjs','native.test.mjs']) fs.copyFileSync(path.join(fixtureRoot,file),path.join(target,'ci',file));
fs.copyFileSync(path.join(fixtureRoot, 'setup.mjs'), path.join(target, 'ci/setup.mjs'));
fs.copyFileSync(path.join(fixtureRoot, 'canary.mjs'), path.join(target, 'ci/canary.mjs'));
fs.writeFileSync(path.join(target, 'ci/scenario.json'), JSON.stringify({scenario})+'\n');
fs.writeFileSync(path.join(target, '.gitignore'), '.ci-local/\n**/reports/\n**/target/\n', { mode: 0o600 });
fs.writeFileSync(path.join(target, '.kd-ci.json'), `${JSON.stringify({ schema_version: 1, concern: 'canary', kit_api: 1, unit_roots: ['rust', 'typescript', 'plugin-alpha', 'plugin-beta', ...nativePlatforms.map(p=>'native-'+p)], shared_units: 'ci/units.json', local_policy: 'developer-machine', results_dir: '.ci-local/results', retention_days: 7 }, null, 2)}\n`, { mode: 0o600 });
fs.writeFileSync(path.join(target, 'ci/units.json'), `${JSON.stringify({ schema: 1, units: [], packs: Object.fromEntries(platforms.map(platform => [platform, { setup: [{ cwd: '.', argv: ['node', 'ci/setup.mjs', platform], timeout_seconds: 30 }], tools: { node: 'v26.8.1' }, ...(support[platform] ? {support: support[platform]} : {}) }])) }, null, 2)}\n`, { mode: 0o600 });
for (const name of ['rust', 'typescript', 'plugin-alpha', 'plugin-beta']) {
  fs.cpSync(path.join(fixtureRoot, name), path.join(target, name), { recursive: true });
}
for (const name of ['rust', 'typescript', 'plugin-alpha', 'plugin-beta']) {
  if (name !== 'typescript' && name !== 'rust') {
    fs.copyFileSync(path.join(fixtureRoot, 'typescript/sum.ts'), path.join(target, name, 'sum.ts'));
    fs.copyFileSync(path.join(fixtureRoot, 'typescript/sum.test.ts'), path.join(target, name, 'sum.test.ts'));
  }
  fs.mkdirSync(path.join(target, name, 'reports'), { mode: 0o700 });
  const census = name === 'rust' ? ['rust/locked unit'] : ['arithmetic/adds whole numbers'];
  const descriptor = {
    schema: 1, id: name, owner: 'test-maintainers', paths: [`${name}/**`],
    test_paths: [name === 'rust' ? 'rust/rust.test.mjs' : `${name}/*.test.ts`], inputs: [name === 'rust' ? 'rust/Cargo.lock' : `${name}/package-lock.json`],
    platforms: hostPlatforms,
    required_in: ['affected', 'full', 'candidate'], timeout_seconds: 300,
    resources: { weight: 1 }, needs: [], depends_on: [], optional_contracts: [],
    cache: { policy: 'none' }, assertions: [], outputs: { visibility: 'private-runner-only' },
    pack: 'workspace', cost_class: 'quick', checks: {
      unit: { census, paths: name === 'rust' ? ['rust/src/**','rust/Cargo.toml','rust/rust.test.mjs'] : [`${name}/*.ts`], test_paths: [name === 'rust' ? 'rust/rust.test.mjs' : `${name}/*.test.ts`], inputs: [],
        platforms: hostPlatforms,
        commands: { default: [{ cwd: '.', argv: ['node', 'ci/canary.mjs'], timeout_seconds: 10 }, { cwd: name, argv: ['node', '--test', '--test-reporter=junit', '--test-reporter-destination=reports/unit.xml', name === 'rust' ? 'rust.test.mjs' : 'sum.test.ts'] }] },
        needs: [], depends_on: [], variants: [], assertions: census, reports: [`${name}/reports/unit.xml`] },
    },
  };
  fs.writeFileSync(path.join(target, name, 'ci-unit.json'), `${JSON.stringify(descriptor, null, 2)}\n`, { mode: 0o600 });
}

for (const platform of nativePlatforms) {
  const name = 'native-'+platform;
  fs.mkdirSync(path.join(target,name),{mode:0o700});
  fs.writeFileSync(path.join(target,name,'README.md'),'# Synthetic venue unit\n\nExercises native readiness with captured output.\n');
  let variants = platform==='ios-simulator' ? [{id:'phone',value:'phone'},{id:'tablet',value:'tablet'}] : platform==='android-emulator' ? (support[platform]?.android?.images || [{variant:'api35',api:35},{variant:'api36',api:36}]).map(v=>({id:v.variant,value:'api'+v.api})) : ['chromium','firefox','webkit'].map(id=>({id,value:id}));
  if (platform==='ios-simulator' && support[platform]?.ios?.devices) variants = support[platform].ios.devices.map(v=>({id:v.variant,value:v.family==='iphone'?'phone':'tablet'}));
  const census=['venue/readiness'];
  const descriptor={schema:1,id:name,owner:'test-maintainers',paths:[name+'/**','ci/native*'],test_paths:['ci/native.test.mjs'],inputs:['package-lock.json'],platforms:[platform],required_in:['affected','full','candidate'],timeout_seconds:300,resources:{weight:1},needs:[],depends_on:[],optional_contracts:[],cache:{policy:'none'},assertions:[],outputs:{visibility:'private-runner-only'},pack:'workspace',cost_class:'normal',checks:{unit:{census,paths:[name+'/**','ci/native*'],test_paths:['ci/native.test.mjs'],inputs:[],platforms:[platform],commands:{default:[{cwd:'.',argv:['node','ci/canary.mjs'],timeout_seconds:10},{cwd:'.',argv:['node','ci/native.mjs',platform]}]},needs:[],depends_on:[],variants:variants.map(v=>({id:v.id,axes:{venue:v.id},args:[v.value]})),assertions:census,reports:[name+'/reports/unit.xml']}}};
  fs.writeFileSync(path.join(target,name,'ci-unit.json'),JSON.stringify(descriptor,null,2)+'\n',{mode:0o600});
}

// The optional leak fixture is provided out of band and is emitted only by the captured fixture commands.
if (process.env.CI_SYNTHETIC_MARKER_FILE) {
  const markerStat = fs.lstatSync(process.env.CI_SYNTHETIC_MARKER_FILE);
  if (!markerStat.isFile() || markerStat.size < 1 || markerStat.size > 65536) throw new Error('invalid marker input');
  const markers = fs.readFileSync(process.env.CI_SYNTHETIC_MARKER_FILE);
  if (markers.length === 0 || markers.length > 65536) throw new Error('invalid marker input');
  fs.writeFileSync(path.join(target, 'private-test-content.txt'), markers, { mode: 0o600 });
}
