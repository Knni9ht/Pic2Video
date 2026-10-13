// Integration check against a running mock server. Uses Node.js built-ins only.
import assert from 'node:assert/strict';
import { deflateSync } from 'node:zlib';
import { mkdir, writeFile } from 'node:fs/promises';

const base = process.env.SMOKE_URL || 'http://127.0.0.1:8080';
const password = process.env.APP_PASSWORD;
assert(password, 'Set APP_PASSWORD to the running server password.');
function crc32(buf) {
  let crc = 0xffffffff;
  for (const byte of buf) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return (crc ^ 0xffffffff) >>> 0;
}
function chunk(type, data) {
  const name = Buffer.from(type), size = Buffer.alloc(4), crc = Buffer.alloc(4);
  size.writeUInt32BE(data.length);
  crc.writeUInt32BE(crc32(Buffer.concat([name, data])));
  return Buffer.concat([size, name, data, crc]);
}
function samplePNG() {
  const width = 320, height = 320, header = Buffer.alloc(13);
  header.writeUInt32BE(width); header.writeUInt32BE(height, 4); header[8] = 8; header[9] = 2;
  const raw = Buffer.alloc((width * 3 + 1) * height);
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const pos = y * (width * 3 + 1) + 1 + x * 3;
    const bottle = x > 105 && x < 215 && y > 65 && y < 265;
    raw[pos] = bottle ? 32 : 237;
    raw[pos + 1] = bottle ? 93 : 244;
    raw[pos + 2] = bottle ? 71 : 239;
  }
  return Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]), chunk('IHDR', header), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}
const png = samplePNG();
await mkdir('.cache', { recursive: true });
await writeFile('.cache/smoke-product.png', png);
let cookie = '';
async function request(path, options = {}, auth = true) {
  return fetch(base + path, { ...options, headers: { ...(auth && cookie ? { Cookie: cookie } : {}), ...options.headers } });
}
const anonymous = await request('/api/jobs', {}, false);
assert.equal(anonymous.status, 401);
const login = await request('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password }) });
assert.equal(login.status, 200);
cookie = login.headers.get('set-cookie')?.split(';')[0] || '';
assert(cookie, 'Expected session cookie');
const session = await (await request('/api/session')).json();
assert.equal(session.authenticated, true);
assert.equal(session.provider, 'mock', 'Smoke tests must only run in mock mode.');
function form(description, bytes = png, filename = 'product.png') {
  const data = new FormData();
  data.set('image', new Blob([bytes], { type: 'image/png' }), filename);
  data.set('description', description); data.set('duration', '5'); data.set('ratio', '9:16'); data.set('resolution', '720p');
  return data;
}
const invalid = await request('/api/jobs', { method: 'POST', body: form('Invalid fixture', Buffer.from('not an image')), headers: { 'Idempotency-Key': crypto.randomUUID() } });
assert.equal(invalid.status, 400);
const key = crypto.randomUUID();
const create = await request('/api/jobs', { method: 'POST', body: form('简约绿色旅行瓶，展示商品外观。'), headers: { 'Idempotency-Key': key } });
assert.equal(create.status, 202, await create.clone().text());
const job = await create.json();
const repeat = await request('/api/jobs', { method: 'POST', body: form('简约绿色旅行瓶，展示商品外观。'), headers: { 'Idempotency-Key': key } });
assert([200, 202].includes(repeat.status));
assert.equal((await repeat.json()).id, job.id);
assert.equal((await request(`/api/jobs/${job.id}/image`, {}, false)).status, 401);
assert.equal((await request(`/api/jobs/${job.id}/video`, {}, false)).status, 401);
assert.equal((await request(`/api/jobs/${job.id}`, { method: 'DELETE' })).status, 409);
async function waitFor(id, desired) {
  for (let i = 0; i < 40; i++) {
    const result = await request(`/api/jobs/${id}`);
    assert.equal(result.status, 200);
    const current = await result.json();
    if (current.status === desired) return current;
    assert(!['failed', 'unknown'].includes(current.status), current.error);
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  assert.fail(`Timed out waiting for ${desired}`);
}
await waitFor(job.id, 'succeeded');
const video = await request(`/api/jobs/${job.id}/video`, { headers: { Range: 'bytes=0-31' } });
assert.equal(video.status, 206);
assert.equal((await video.arrayBuffer()).byteLength, 32);
const download = await request(`/api/jobs/${job.id}/video?download=1`);
assert.equal(download.status, 200);
assert(download.headers.get('content-disposition')?.includes('attachment'));
const list = await (await request('/api/jobs?page=1&page_size=12')).json();
assert(list.items.some(item => item.id === job.id));
const failedCreate = await request('/api/jobs', { method: 'POST', body: form('[模拟失败] 验证错误提示'), headers: { 'Idempotency-Key': crypto.randomUUID() } });
assert.equal(failedCreate.status, 202);
const failedJob = await failedCreate.json();
const failure = await waitFor(failedJob.id, 'failed');
assert(failure.error);
for (const id of [job.id, failedJob.id]) {
  assert.equal((await request(`/api/jobs/${id}`, { method: 'DELETE' })).status, 204);
  assert.equal((await request(`/api/jobs/${id}`)).status, 404);
}
assert.equal((await request('/api/logout', { method: 'POST' })).status, 204);
assert.equal((await request('/api/jobs')).status, 401);
console.log('PASS: auth, upload validation, idempotency, async success/failure, protected media, Range, download, history, delete, logout.');
