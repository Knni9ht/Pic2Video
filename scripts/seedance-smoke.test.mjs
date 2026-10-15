// Local HTTP fixtures only. These tests cannot create a real Ark task.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const script = fileURLToPath(new URL('./seedance-smoke.mjs', import.meta.url));
const video = Buffer.alloc(64);
video.writeUInt32BE(24); video.write('ftyp', 4); video.write('isom', 8);

async function fixture(t, settings = {}) {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'pic2video-seedance-smoke-'));
  await writeFile(path.join(directory, 'product.png'), 'local fixture image');
  const calls = [];
  const keys = new Set();
  let count = 0;
  const server = createServer(async (req, res) => {
    let bytes = [];
    for await (const chunk of req) bytes.push(chunk);
    const body = Buffer.concat(bytes).toString();
    calls.push({ method: req.method, url: req.url, key: req.headers['idempotency-key'], body });
    const json = (status, value) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
    const job = { id: 'local-fixture', provider: 'seedance', duration: 5, ratio: 'adaptive', resolution: '720p', status: settings.status || 'succeeded', error: settings.error || '', created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:01Z' };
    if (req.url === '/api/login') {
      res.setHeader('Set-Cookie', 'pic2video_session=fixture; HttpOnly; SameSite=Lax; Path=/');
      json(200, { authenticated: true, provider: settings.provider || 'seedance' });
    } else if (req.url === '/api/session') {
      json(200, { authenticated: true, provider: settings.provider || 'seedance' });
    } else if (req.url === '/api/logout') {
      res.writeHead(204); res.end();
    } else if (req.method === 'DELETE') {
      json(500, { error: { message: 'Script must not delete retained tasks.' } });
    } else if (req.url === '/api/jobs' && req.method === 'POST') {
      if (!keys.has(req.headers['idempotency-key'])) { keys.add(req.headers['idempotency-key']); count++; }
      if (settings.uncertain) { res.destroy(); return; }
      json(202, job);
    } else if (req.url === '/api/jobs/local-fixture') {
      json(200, job);
    } else if (/\/api\/jobs\/local-fixture\/(image|video)/.test(req.url)) {
      if (!req.headers.cookie) { json(401, { error: { message: 'Login required' } }); return; }
      if (req.headers.range) { res.writeHead(206, { 'Content-Range': 'bytes 0-31/64' }); res.end(video.subarray(0, 32)); }
      else { res.writeHead(200, { 'Content-Type': 'video/mp4', 'Content-Disposition': 'attachment; filename=fixture.mp4' }); res.end(settings.corruptDownload ? Buffer.alloc(64) : video); }
    } else json(404, {});
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  t.after(async () => {
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
    assert(path.resolve(directory).startsWith(path.resolve(os.tmpdir()) + path.sep) && path.basename(directory).startsWith('pic2video-seedance-smoke-'), 'Refusing to remove a directory outside this test fixture.');
    await rm(directory, { recursive: true, force: true });
  });
  async function run(args = [], environment = {}) {
    const child = spawn(process.execPath, [script, ...args], {
      cwd: directory,
      env: { ...process.env, APP_PASSWORD: 'fixture-password', ARK_API_KEY: 'must-not-leak-secret', ARK_MODEL_ID: 'doubao-seedance-2-0-mini-260615', SMOKE_URL: `http://127.0.0.1:${server.address().port}`, ALLOW_REAL_MODEL_TESTS: '1', ...environment },
      windowsHide: true,
    });
    let output = '';
    child.stdout.on('data', chunk => { output += chunk; });
    child.stderr.on('data', chunk => { output += chunk; });
    const [code] = await once(child, 'close');
    assert(!output.includes('must-not-leak-secret'));
    return { code, output };
  }
  async function record() {
    const root = path.join(directory, '.cache', 'seedance-smoke');
    const entries = await readdir(root);
    assert.equal(entries.length, 1);
    const dir = path.join(root, entries[0]);
    return { dir, value: JSON.parse(await readFile(path.join(dir, 'record.json'), 'utf8')) };
  }
  return { calls, run, record, count: () => count };
}

const generate = ['--generate', '--image', 'product.png', '--description', '展示商品外观。'];

test('default checks login and mode without creating jobs or downloading media', async t => {
  const f = await fixture(t);
  const result = await f.run();
  assert.equal(result.code, 0, result.output);
  assert.deepEqual(f.calls.map(c => c.url), ['/api/login', '/api/session', '/api/logout']);
  assert.equal(f.count(), 0);
});

test('rejects mock mode before creating a job', async t => {
  const f = await fixture(t, { provider: 'mock' });
  const result = await f.run(generate);
  assert.equal(result.code, 1);
  assert.equal(f.count(), 0);
});

test('generate is blocked without an explicit allowance before any HTTP request', async t => {
  const f = await fixture(t);
  const result = await f.run(generate, { ALLOW_REAL_MODEL_TESTS: '0' });
  assert.equal(result.code, 1);
  assert(result.output.includes('ALLOW_REAL_MODEL_TESTS=1'));
  assert.equal(f.calls.length, 0);
});

test('explicit generate replays the same key and retains the task, report and video', async t => {
  const f = await fixture(t);
  const result = await f.run(generate);
  assert.equal(result.code, 0, result.output);
  const submissions = f.calls.filter(c => c.url === '/api/jobs');
  assert.equal(submissions.length, 2);
  assert.equal(submissions[0].key, submissions[1].key);
  assert.equal(f.count(), 1);
  for (const [name, value] of [['duration', '5'], ['ratio', 'adaptive'], ['resolution', '720p']]) {
    assert(submissions[0].body.includes(`name="${name}"\r\n\r\n${value}`));
  }
  assert(!f.calls.some(c => c.method === 'DELETE'));
  const record = await f.record();
  assert.equal(record.value.job_id, 'local-fixture');
  assert.equal(record.value.http_acceptance, 'passed');
  assert.equal(record.value.manual_checks.silent_audio, 'pending');
  assert.deepEqual(await readFile(path.join(record.dir, 'video.mp4')), video);
});

test('existing job mode only queries and downloads without a creation POST', async t => {
  const f = await fixture(t);
  const result = await f.run(['--job-id', 'local-fixture']);
  assert.equal(result.code, 0, result.output);
  assert.equal(f.count(), 0);
  const { value } = await f.record();
  assert.equal(value.mode, 'existing-job');
  assert.equal(value.http_acceptance, 'passed');
});

test('corrupt download is rejected and the partial file is removed', async t => {
  const f = await fixture(t, { corruptDownload: true });
  const result = await f.run(['--job-id', 'local-fixture']);
  assert.equal(result.code, 1);
  const { dir, value } = await f.record();
  assert.equal(value.http_acceptance, 'not-passed');
  assert.deepEqual(await readdir(dir), ['record.json']);
  assert.equal(f.count(), 0);
});

for (const status of ['failed', 'unknown']) {
  test(`${status} stops without creating replacement jobs`, async t => {
    const f = await fixture(t, { status, error: '请先核实已有任务。' });
    const result = await f.run(generate);
    assert.equal(result.code, 1);
    assert.equal(f.count(), 1);
    const { value } = await f.record();
    assert.equal(value.status, status);
    assert.equal(value.http_acceptance, 'not-passed');
    assert.equal(value.job_id, 'local-fixture');
  });
}

test('uncertain creation is not retried and keeps the idempotency key', async t => {
  const f = await fixture(t, { uncertain: true });
  const result = await f.run(generate);
  assert.equal(result.code, 1);
  assert.equal(f.calls.filter(c => c.url === '/api/jobs').length, 1);
  const { value } = await f.record();
  assert.equal(value.status, 'submitting');
  assert(value.idempotency_key);
  assert.equal(value.job_id, null);
});

test('timeout retains the job ID and does not resubmit', async t => {
  const f = await fixture(t, { status: 'running' });
  const result = await f.run([...generate, '--timeout-seconds', '1']);
  assert.equal(result.code, 1);
  assert.equal(f.count(), 1);
  assert(result.output.includes('--job-id local-fixture'));
  const { value } = await f.record();
  assert.equal(value.status, 'running');
});

test('mutually exclusive or incomplete arguments fail before contacting the server', async t => {
  const f = await fixture(t);
  assert.equal((await f.run(['--generate', '--job-id', 'local-fixture'])).code, 1);
  assert.equal((await f.run(['--generate', '--image', 'product.png'])).code, 1);
  assert.equal(f.calls.length, 0);
});
