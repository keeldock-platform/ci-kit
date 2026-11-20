import fs from 'node:fs';
const {scenario} = JSON.parse(fs.readFileSync('ci/scenario.json', 'utf8'));
const marker = fs.existsSync('private-test-content.txt') ? fs.readFileSync('private-test-content.txt', 'utf8') : 'invented-canary-content';
// Deliberately exercise private capture before every declared test command.
process.stdout.write(marker + '\n');
process.stderr.write(marker + '\n');
process.stdout.write('\x1b[31m' + marker + '\x1b[0m\n');
process.stderr.write('::error::' + marker + '\n');
if (scenario === 'failure') process.exitCode = 23;
else if (scenario === 'crash') throw new Error(marker);
else if (scenario === 'timeout' || scenario === 'cancel') setInterval(() => {}, 1000);
