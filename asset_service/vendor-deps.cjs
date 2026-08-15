const https = require('https');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { execSync } = require('child_process');

const ROOT = process.cwd();
const CACHE = path.join(ROOT, '.crate-cache');
const VENDOR = path.join(ROOT, 'vendor');
fs.mkdirSync(CACHE, { recursive: true });
fs.mkdirSync(VENDOR, { recursive: true });

// 1. parse Cargo.lock -> [{name, version}]
const lock = fs.readFileSync(path.join(ROOT, 'Cargo.lock'), 'utf8');
const pkgs = [];
const re = /\[\[package\]\]([\s\S]*?)(?=\n\[\[package\]\]|\n\[metadata\]|$)/g;
let m;
while ((m = re.exec(lock))) {
  const block = m[1];
  const nm = /name = "([^"]+)"/.exec(block);
  const ver = /version = "([^"]+)"/.exec(block);
  const src = /source = "([^"]+)"/.exec(block);
  if (nm && ver && (!src || src[1].startsWith('registry+'))) {
    if (nm[1] === 'node_agent') continue; // 本地 workspace 包，不 vendor
    pkgs.push({ name: nm[1], version: ver[1] });
  }
}
console.log('deps to fetch:', pkgs.length);

// 2. download each crate via ustc mirror (node TLS, avoids schannel issue)
function download(url, dest, redirects) {
  redirects = redirects || 0;
  if (redirects > 5) return Promise.reject(new Error('too many redirects ' + url));
  return new Promise((resolve, reject) => {
    const req = https.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        const next = new URL(res.headers.location, url).toString();
        return resolve(download(next, dest, redirects + 1));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error('HTTP ' + res.statusCode + ' for ' + url));
      }
      const out = fs.createWriteStream(dest);
      res.pipe(out);
      out.on('finish', () => resolve());
      out.on('error', reject);
    });
    req.on('error', reject);
    req.setTimeout(60000, () => { req.destroy(new Error('timeout ' + url)); });
  });
}

(async () => {
  let ok = 0, fail = [];
  for (const p of pkgs) {
    const crateFile = path.join(CACHE, p.name + '-' + p.version + '.crate');
    const targetDir = path.join(VENDOR, p.name + '-' + p.version);
    const checksumPath = path.join(targetDir, '.cargo-checksum.json');
    if (fs.existsSync(checksumPath)) { ok++; continue; }
    try {
      if (!fs.existsSync(crateFile)) {
        await download('https://mirrors.ustc.edu.cn/crates.io/api/v1/crates/' + encodeURIComponent(p.name) + '/' + encodeURIComponent(p.version) + '/download', crateFile);
      }
      fs.mkdirSync(targetDir, { recursive: true });
      execSync('tar -xzf "' + crateFile + '" -C "' + targetDir + '" --strip-components=1', { stdio: 'ignore' });
      const sha = crypto.createHash('sha256').update(fs.readFileSync(crateFile)).digest('hex');
      fs.writeFileSync(checksumPath, JSON.stringify({ files: {}, package: sha }, null, 2));
      ok++;
      if (ok % 20 === 0) console.log('progress:', ok + '/' + pkgs.length);
    } catch (e) {
      fail.push(p.name + '-' + p.version + ': ' + e.message);
      console.log('FAIL', p.name + '-' + p.version, e.message);
    }
  }
  console.log('done. ok=' + ok + ' total=' + pkgs.length + ' fail=' + fail.length);
  if (fail.length) console.log(fail.join('\n'));
})();
