// Opt-in real-provider HTTP acceptance. Uses Node.js built-ins; never calls Ark directly.
import assert from 'node:assert/strict';
import { randomUUID, createHash } from 'node:crypto';
import { createWriteStream } from 'node:fs';
import { mkdir, readFile, writeFile, rename, rm, stat } from 'node:fs/promises';
import path from 'node:path';
import { Readable, Transform } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import { parseArgs } from 'node:util';

const help = `Seedance 2.0 mini 本地验收（先启动配置为 seedance 的服务）
  node --env-file=.env scripts/seedance-smoke.mjs
  node --env-file=.env scripts/seedance-smoke.mjs --generate --image 商品.png --description "商品外观与运镜要求"
  node --env-file=.env scripts/seedance-smoke.mjs --job-id 已有本地任务ID

默认仅检查登录和 Seedance 运行模式，不创建生成任务。
--generate 创建一条 5 秒、720p、跟随原图、静音 MP4 任务，可能产生费用。
保留模型额度：日常测试使用 Mock 或本地替身；仅获用户明确授权后临时设置 ALLOW_REAL_MODEL_TESTS=1 并使用 --generate。
--job-id 只查询已有任务和下载其已保存的视频，不创建或恢复远程任务。
--timeout-seconds 默认 900（15 分钟），超时后后台任务继续，使用 --job-id 再查。
APP_PASSWORD 必须与服务一致；SMOKE_URL 默认 http://127.0.0.1:8080。
验收记录及视频保存在 .cache/seedance-smoke/；任务和素材保留，不自动删除。`;

let record;
let runDir;
let cookie = '';
let base;

async function saveRecord() {
  if (record && runDir) {
    record.updated_at = new Date().toISOString();
    // A partial write must not destroy the last usable task ID.
    const temporary = path.join(runDir, 'record.json.part');
    await writeFile(temporary, JSON.stringify(record, null, 2) + '\n');
    await rename(temporary, path.join(runDir, 'record.json'));
  }
}

async function request(route, options = {}, authenticated = true) {
  return fetch(base + route, {
    ...options,
    redirect: 'error',
    signal: AbortSignal.timeout(30_000),
    headers: { ...(authenticated && cookie ? { Cookie: cookie } : {}), ...options.headers },
  });
}

async function jsonResponse(response, expectedStatus) {
  const body = await response.json().catch(() => null);
  assert.equal(response.status, expectedStatus, body?.error?.message || `接口返回 HTTP ${response.status}`);
  assert(body && typeof body === 'object', '接口返回格式异常。');
  return body;
}

function jobRoute(id) {
  assert(typeof id === 'string' && /^[a-zA-Z0-9_-]{1,128}$/.test(id), '本地任务 ID 格式无效。');
  return `/api/jobs/${encodeURIComponent(id)}`;
}

async function downloadVideo(route) {
  const range = await request(`${route}/video`, { headers: { Range: 'bytes=0-31' } });
  assert.equal(range.status, 206, '视频 Range 请求未成功。');
  const header = Buffer.from(await range.arrayBuffer());
  assert.equal(header.length, 32, '视频 Range 响应长度异常。');
  assert.equal(header.toString('ascii', 4, 8), 'ftyp', '视频不是有效的 MP4。');
  const download = await request(`${route}/video?download=1`);
  assert.equal(download.status, 200, '视频下载未成功。');
  assert(download.headers.get('content-disposition')?.includes('attachment'), '下载接口缺少附件头。');
  assert(download.body, '视频下载为空。');
  const temporary = path.join(runDir, 'video.mp4.part');
  const destination = path.join(runDir, 'video.mp4');
  const hash = createHash('sha256');
  let bytes = 0;
  const leadingBytes = [];
  let leadingLength = 0;
  const limit = new Transform({
    transform(chunk, encoding, callback) {
      bytes += chunk.length;
      if (bytes > 512 * 1024 * 1024) return callback(new Error('视频超过 512 MB 保存上限。'));
      if (leadingLength < 32) {
        const part = chunk.subarray(0, 32 - leadingLength);
        leadingBytes.push(part);
        leadingLength += part.length;
      }
      hash.update(chunk);
      callback(null, chunk);
    },
  });
  try {
    await pipeline(Readable.fromWeb(download.body), limit, createWriteStream(temporary, { flags: 'wx' }));
    assert(bytes >= 32, '视频文件不完整。');
    assert(Buffer.concat(leadingBytes).equals(header), '下载视频头与 Range 响应不一致。');
    await rename(temporary, destination);
  } finally {
    await rm(temporary, { force: true });
  }
  return { file: 'video.mp4', bytes, sha256: hash.digest('hex'), range: 'passed' };
}

async function main() {
  const { values } = parseArgs({ options: {
    help: { type: 'boolean' }, generate: { type: 'boolean' }, image: { type: 'string' },
    description: { type: 'string' }, 'job-id': { type: 'string' }, 'timeout-seconds': { type: 'string' },
  } });
  if (values.help) { console.log(help); return; }
  assert(!(values.generate && values['job-id']), '--generate 与 --job-id 不能同时使用。');
  assert(values.generate || (!values.image && !values.description), '--image 和 --description 仅用于 --generate。');
  const timeout = Number(values['timeout-seconds'] ?? 900);
  assert(Number.isInteger(timeout) && timeout >= 1 && timeout <= 7200, '--timeout-seconds 必须在 1–7200 之间。');
  if (values['job-id']) jobRoute(values['job-id']);
  let image;
  let filename;
  let mime;
  let description;
  if (values.generate) {
    assert(process.env.ALLOW_REAL_MODEL_TESTS === '1', '为保留模型额度，真实生成测试默认禁用。仅在用户明确授权本轮真实调用后，临时设置 ALLOW_REAL_MODEL_TESTS=1 并使用 --generate。');
    assert(values.image, '--generate 必须指定 --image 商品图片路径。');
    description = values.description?.trim();
    assert(description && [...description].length <= 2000, '--generate 必须指定 1–2000 字符的 --description。');
    filename = path.basename(values.image);
    mime = { '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp' }[path.extname(filename).toLowerCase()];
    assert(mime, '商品图片须为 JPG、PNG 或 WebP。');
    const info = await stat(values.image);
    assert(info.isFile() && info.size > 0 && info.size <= 10 * 1024 * 1024, '图片不可为空且不能超过 10 MB。');
    image = await readFile(values.image);
  }
  assert(process.env.APP_PASSWORD, '请设置 APP_PASSWORD，或使用 node --env-file=.env。');
  const url = new URL(process.env.SMOKE_URL || 'http://127.0.0.1:8080');
  assert(['http:', 'https:'].includes(url.protocol) && !url.username && !url.password && !url.search && !url.hash && url.pathname === '/', 'SMOKE_URL 须为无凭据的 HTTP(S) 服务根地址。');
  base = url.origin;
  const login = await request('/api/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: process.env.APP_PASSWORD }),
  }, false);
  const session = await jsonResponse(login, 200);
  cookie = login.headers.get('set-cookie')?.split(';')[0] || '';
  assert(cookie, '登录没有返回会话 Cookie。');
  assert(session.authenticated && session.provider === 'seedance', '验收脚本仅用于已登录的 Seedance 服务，请检查 VIDEO_PROVIDER 并重启。');
  const currentSession = await jsonResponse(await request('/api/session'), 200);
  assert(currentSession.authenticated && currentSession.provider === 'seedance', '登录状态或运行模式校验失败。');
  console.log('PASS：登录与 Seedance 运行模式检查通过。');
  if (!values.generate && !values['job-id']) {
    console.log('未创建生成任务；这不代表 API Key、模型权限或账户额度已通过真实调用验证。');
    return;
  }
  runDir = path.resolve('.cache', 'seedance-smoke', `${Date.now()}-${randomUUID()}`);
  await mkdir(runDir, { recursive: true });
  const started = Date.now();
  record = {
    mode: values.generate ? 'generate' : 'existing-job', started_at: new Date().toISOString(),
    model_id_from_environment: process.env.ARK_MODEL_ID || null,
    model_id_note: '需与服务提交日志中的 model 核对；脚本不查询方舟。',
    status: 'checking', job_id: values['job-id'] || null,
    requested: values.generate ? { duration: 5, resolution: '720p', ratio: 'adaptive', generate_audio: false, expected_output_format: 'mp4' } : null,
    manual_checks: { duration_and_resolution: 'pending', aspect_follows_image: 'pending', silent_audio: 'pending', product_appearance: 'pending', browser_playback_refresh_restart: 'pending' },
  };
  await saveRecord();
  console.log(`验收记录目录：${runDir}`);
  if (values.generate) {
    const key = randomUUID();
    record.idempotency_key = key;
    record.status = 'submitting';
    await saveRecord();
    function form() {
      const data = new FormData();
      data.set('image', new Blob([image], { type: mime }), filename);
      data.set('description', description); data.set('duration', '5');
      data.set('ratio', 'adaptive'); data.set('resolution', '720p');
      return data;
    }
    // Never retry an uncertain creation request, even with the same key.
    const job = await jsonResponse(await request('/api/jobs', {
      method: 'POST', body: form(), headers: { 'Idempotency-Key': key },
    }), 202);
    jobRoute(job.id);
    record.job_id = job.id;
    record.status = job.status;
    await saveRecord();
    console.log(`本地任务 ID：${job.id}；后续使用 --job-id ${job.id}，不要再次 --generate。`);
    const repeated = await jsonResponse(await request('/api/jobs', {
      method: 'POST', body: form(), headers: { 'Idempotency-Key': key },
    }), 202);
    assert.equal(repeated.id, job.id, '相同幂等键未返回原任务。');
    record.idempotency_check = 'passed';
    await saveRecord();
  }
  const route = jobRoute(record.job_id);
  const deadline = Date.now() + timeout * 1000;
  let previousStatus;
  let job;
  while (true) {
    job = await jsonResponse(await request(route), 200);
    assert.equal(job.provider, 'seedance', '--job-id 必须指向 Seedance 本地任务。');
    record.status = job.status;
    record.elapsed_seconds = Number(((Date.now() - started) / 1000).toFixed(2));
    record.job = { id: job.id, status: job.status, duration: job.duration, ratio: job.ratio, resolution: job.resolution, created_at: job.created_at, updated_at: job.updated_at };
    await saveRecord();
    if (job.status !== previousStatus) { console.log(`任务状态：${job.status}`); previousStatus = job.status; }
    assert(!['failed', 'unknown'].includes(job.status), `${job.error || '任务未成功。'} 不会自动重新生成，请核查已有任务。`);
    if (job.status === 'succeeded') break;
    assert(Date.now() < deadline, `等待超时，后台任务继续。请使用 --job-id ${record.job_id} 查询，不要重新生成。`);
    await new Promise(resolve => setTimeout(resolve, Math.min(2000, Math.max(1, deadline - Date.now()))));
  }
  if (values.generate) {
    assert.equal(job.duration, 5); assert.equal(job.resolution, '720p'); assert.equal(job.ratio, 'adaptive');
  }
  assert.equal((await request(`${route}/image`, {}, false)).status, 401, '图片未受登录保护。');
  assert.equal((await request(`${route}/video`, {}, false)).status, 401, '视频未受登录保护。');
  record.media_auth = 'passed';
  record.video = await downloadVideo(route);
  record.http_acceptance = 'passed';
  record.elapsed_seconds = Number(((Date.now() - started) / 1000).toFixed(2));
  await saveRecord();
  console.log('PASS：任务成功、媒体鉴权、Range 播放接口、MP4 下载检查通过；任务和视频已保留。');
  console.log('请在网页或媒体工具中确认实际时长、720p、画幅、静音和商品外观，并检查页面刷新与服务重启后的访问。');
}

try {
  await main();
} catch (error) {
  const message = error instanceof Error ? error.message : '验收未完成。';
  if (record) {
    record.http_acceptance = 'not-passed';
    record.error = message;
    await saveRecord().catch(() => {});
  }
  console.error(`FAIL：${message}`);
  if (record?.job_id) console.error(`已有任务 ${record.job_id}，使用 --job-id 查询；不会重新生成。`);
  else if (record?.status === 'submitting') console.error(`提交结果未能确认，请先核实服务任务列表和后台日志。幂等键已保存在 ${runDir}，不要重新生成。`);
  process.exitCode = 1;
} finally {
  if (cookie) await request('/api/logout', { method: 'POST' }).catch(() => {});
}
