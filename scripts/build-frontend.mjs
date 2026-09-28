#!/usr/bin/env node
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const root = resolve(import.meta.dirname, '..');
const frontend = join(root, 'frontend');
const output = join(root, 'internal/ui/webdist');
const stamp = join(root, '.build/frontend.json');
const assets = ['index.html', 'assets/app.js', 'assets/index.css'];
const force = process.argv.includes('--force') || process.env.FORCE_FRONTEND_BUILD === '1';

function files(directory) {
  return readdirSync(directory, { withFileTypes: true })
    .filter((entry) => !['node_modules', '.vite'].includes(entry.name))
    .flatMap((entry) => {
      const path = join(directory, entry.name);
      return entry.isDirectory() ? files(path) : entry.isFile() ? [path] : [];
    });
}

function digest(paths) {
  const hash = createHash('sha256');
  for (const path of paths.sort()) {
    hash.update(relative(root, path));
    hash.update('\0');
    hash.update(readFileSync(path));
    hash.update('\0');
  }
  return hash.digest('hex');
}

function outputsDigest() {
  const paths = assets.map((path) => join(output, path));
  return paths.every(existsSync) ? digest(paths) : null;
}

function run(...args) {
  const result = spawnSync(process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm', args, {
    cwd: root,
    stdio: 'inherit',
    shell: process.platform === 'win32',
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status || 1);
}

const source = digest([...files(frontend), import.meta.filename]);
let cached;
try {
  cached = JSON.parse(readFileSync(stamp, 'utf8'));
} catch {
  cached = null;
}
if (!force && cached?.source === source && cached?.output === outputsDigest()) {
  console.log('Frontend unchanged; using cached assets.');
  process.exit(0);
}
run('-C', 'frontend', 'install', '--frozen-lockfile');
run('-C', 'frontend', 'run', 'build');
const built = outputsDigest();
if (!built) throw new Error('frontend build did not create the expected assets');
mkdirSync(join(root, '.build'), { recursive: true });
writeFileSync(stamp, JSON.stringify({ source, output: built }) + '\n');
