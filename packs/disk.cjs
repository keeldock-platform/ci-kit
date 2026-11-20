'use strict';
const fs = require('node:fs');
try {
  if (!/^[1-9][0-9]{0,12}$/.test(process.argv[2] || '')) process.exit(2);
  const working = BigInt(process.argv[2]);
  const space = fs.statfsSync(process.cwd(), {bigint: true});
  const available = space.bavail * space.bsize;
  const allocation = available;
  if (working < 1073741824n || available < working || available - working < allocation / 5n) process.exit(1);
} catch { process.exit(1); }
