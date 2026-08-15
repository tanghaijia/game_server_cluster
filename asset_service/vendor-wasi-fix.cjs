const https = require('https');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { execSync } = require('child_process');
const ROOT = process.cwd();
const CACHE = path.join(ROOT, '.crate-cache');
const VENDOR = path.join(ROOT, 'vendor');
const missing = [
  ['wasi', '0.11.1+wasi-snapshot-preview1'],
  ['wasip2', '1.0.3+wasi-0.2.9'],
  ['wasip3', '0.4.0+wasi-0.3.0-rc-2026-01-06'],
];
function download(url, dest) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, (res) => {
      if (res.statusCode !== 200) { res.resume(); return reject(new Error('HTTP ' + res.statusCode)); }
      const out = fs.createWriteStream(dest);
      res.pipe(out);
      out.on('finish', () => resolve());
      out.on('error', reject);
    });
    req.on('error', reject);
    req.setTimeout(60000, () => req.destroy(new Error('timeout')));
  });
}
(async () => {
  for (const [name, version] of missing) {
    const crateFile = path.join(CACHE, name + '-' + version + '.crate');
    const targetDir = path.join(VENDOR, name + '-' + version);
    const encName = encodeURIComponent(name);
    const encVer = encodeURIComponent(version);
    try {
      if (!fs.existsSync(crateFile)) {
        await download('https://static.crates.io/crates/' + encName + '/' + encName + '-' + encVer + '.crate', crateFile);
      }
      fs.mkdirSync(targetDir, { recursive: true });
      execSync('tar -xzf "' + crateFile + '" -C "' + targetDir + '" --strip-components=1', { stdio: 'ignore' });
      const sha = crypto.createHash('sha256').update(fs.readFileSync(crateFile)).digest('hex');
      fs.writeFileSync(path.join(targetDir, '.cargo-checksum.json'), JSON.stringify({ files: {}, package: sha }, null, 2));
      console.log('OK', name + '-' + version);
    } catch (e) { console.log('FAIL', name + '-' + version, e.message); }
  }
})();
