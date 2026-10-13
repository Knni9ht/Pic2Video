<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
import { APIError, jobImage, jobVideo, newRequestKey, request } from './api'
import type { Job, JobPage, Provider, Session, Status } from './api'

const ready = ref(false)
const authenticated = ref(false)
const provider = ref<Provider>('mock')
const password = ref('')
const loginBusy = ref(false)
const loginError = ref('')
const tab = ref<'studio' | 'history'>('studio')
const file = ref<File | null>(null)
const previewURL = ref('')
const description = ref('')
const duration = ref(5)
const ratio = ref('9:16')
const dragging = ref(false)
const imageInput = ref<HTMLInputElement | null>(null)
const submitting = ref(false)
const alreadySubmitted = ref(false)
const formError = ref('')
const notice = ref('')
const requestKey = ref(newRequestKey())
const jobs = ref<Job[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 12
const historyLoading = ref(false)
const historyError = ref('')
const selected = ref<Job | null>(null)
const pollWarning = ref('')
const retryBusy = ref(false)
const deleting = ref(false)
const deleteTarget = ref<Job | null>(null)
const deleteDialog = ref<HTMLDialogElement | null>(null)
const deleteError = ref('')
let pollTimer: ReturnType<typeof setInterval> | undefined
let polling = false
let historySequence = 0
let fileSequence = 0

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const active = (job: Job) => ['queued', 'submitting', 'running', 'downloading'].includes(job.status)
const canSubmit = computed(() => Boolean(file.value && description.value.trim()) && !submitting.value && !alreadySubmitted.value)
const labels: Record<Status, string> = { queued: '等待开始', submitting: '正在提交', running: '正在生成', downloading: '正在保存', succeeded: '已完成', failed: '生成失败', unknown: '需要确认' }
const statusText = (job: Job) => labels[job.status] || '状态更新中'
const selectedStep = computed(() => selected.value ? ({ queued: 0, submitting: 0, running: 1, downloading: 2, succeeded: 3, failed: -1, unknown: -1 }[selected.value.status]) : -1)
const steps = ['任务排队', '生成视频', '保存视频', '制作完成']
const dateText = (value: string) => new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value))

watch([file, description, duration, ratio], () => { requestKey.value = newRequestKey(); alreadySubmitted.value = false; notice.value = '' }, { flush: 'sync' })
watch(file, (value) => {
  if (previewURL.value) URL.revokeObjectURL(previewURL.value)
  previewURL.value = value ? URL.createObjectURL(value) : ''
})
watch(page, () => { void loadHistory() })

function message(error: unknown): string {
  if (error instanceof APIError && error.status === 401 && authenticated.value) {
    authenticated.value = false
    selected.value = null
    jobs.value = []
    loginError.value = '登录已过期，请重新输入共享密码。'
  }
  return error instanceof Error ? error.message : '操作未完成，请稍后重试。'
}

async function initialize() {
  try {
    const session = await request<Session>('/api/session')
    authenticated.value = session.authenticated
    provider.value = session.provider
    if (session.authenticated) await loadHistory()
  } catch (error) { loginError.value = message(error) }
  finally { ready.value = true }
}

async function login() {
  if (!password.value || loginBusy.value) return
  loginBusy.value = true
  loginError.value = ''
  try {
    const session = await request<Session>('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: password.value }) })
    authenticated.value = session.authenticated
    provider.value = session.provider
    password.value = ''
    await loadHistory()
  } catch (error) { loginError.value = message(error) }
  finally { loginBusy.value = false }
}

async function logout() {
  try {
    await request<void>('/api/logout', { method: 'POST' })
    authenticated.value = false
    selected.value = null
    jobs.value = []
    file.value = null
    description.value = ''
    notice.value = ''
    formError.value = ''
    loginError.value = ''
  } catch (error) { formError.value = message(error) }
}

async function acceptFile(candidate?: File) {
  if (!candidate || submitting.value) return
  formError.value = ''
  if (candidate.size > 10 * 1024 * 1024) { formError.value = '图片不能超过 10 MB，请压缩后重试。'; return }
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(candidate.type)) { formError.value = '请选择 JPG、PNG 或 WebP 格式的商品图片。'; return }
  const current = ++fileSequence
  const url = URL.createObjectURL(candidate)
  try {
    await new Promise<void>((resolve, reject) => {
      const image = new Image()
      image.onload = () => image.naturalWidth > 0 && image.naturalHeight > 0 ? resolve() : reject(new Error('图片尺寸无效。'))
      image.onerror = () => reject(new Error('无法读取这张图片，请选择有效的商品图片。'))
      image.src = url
    })
    if (current === fileSequence) file.value = candidate
  } catch (error) { if (current === fileSequence) formError.value = message(error) }
  finally { URL.revokeObjectURL(url); if (imageInput.value) imageInput.value.value = '' }
}

function drop(event: DragEvent) {
  dragging.value = false
  if ((event.dataTransfer?.files.length ?? 0) > 1) { formError.value = '每次只支持一张商品图片。'; return }
  void acceptFile(event.dataTransfer?.files[0])
}

async function submit() {
  if (!canSubmit.value || !file.value) return
  submitting.value = true
  formError.value = ''
  notice.value = ''
  const data = new FormData()
  data.append('image', file.value)
  data.append('description', description.value.trim())
  data.append('duration', String(duration.value))
  data.append('ratio', ratio.value)
  data.append('resolution', '720p')
  try {
    selected.value = await request<Job>('/api/jobs', { method: 'POST', headers: { 'Idempotency-Key': requestKey.value }, body: data })
    alreadySubmitted.value = true
    notice.value = '任务已提交，关闭页面后会继续处理。修改素材后可创建新任务。'
    if (page.value !== 1) page.value = 1
    else await loadHistory()
  } catch (error) { formError.value = message(error) }
  finally { submitting.value = false }
}

async function fetchHistory() {
  const sequence = ++historySequence
  const result = await request<JobPage>(`/api/jobs?page=${page.value}&page_size=${pageSize}`)
  if (sequence !== historySequence || !authenticated.value) return
  jobs.value = result.items ?? []
  total.value = result.total
  if (selected.value) {
    const updated = jobs.value.find((job) => job.id === selected.value?.id)
    if (updated) selected.value = updated
  }
}

async function loadHistory() {
  historyLoading.value = true
  historyError.value = ''
  try { await fetchHistory() }
  catch (error) { historyError.value = message(error) }
  finally { historyLoading.value = false }
}

async function poll() {
  if (!authenticated.value || polling || document.visibilityState === 'hidden') return
  polling = true
  try {
    await fetchHistory()
    if (selected.value && active(selected.value) && !jobs.value.some((job) => job.id === selected.value?.id)) {
      const id = selected.value.id
      const updated = await request<Job>(`/api/jobs/${encodeURIComponent(id)}`)
      if (selected.value?.id === id) selected.value = updated
    }
    pollWarning.value = ''
  } catch (error) { pollWarning.value = message(error) }
  finally { polling = false }
}

function openJob(job: Job) {
  selected.value = job
  tab.value = 'studio'
  void nextTick(() => document.getElementById('preview')?.scrollIntoView({ behavior: 'smooth', block: 'nearest' }))
}

async function prepareRetry(job: Job) {
  retryBusy.value = true
  formError.value = ''
  try {
    const response = await fetch(jobImage(job), { credentials: 'same-origin' })
    if (!response.ok) throw new APIError(response.status, 'image_unavailable', '原始图片读取失败，请重新上传。')
    const blob = await response.blob()
    const extension = blob.type === 'image/png' ? 'png' : blob.type === 'image/webp' ? 'webp' : 'jpg'
    file.value = new File([blob], `商品图片.${extension}`, { type: blob.type })
    description.value = job.description
    duration.value = job.duration
    ratio.value = job.ratio
    requestKey.value = newRequestKey()
    alreadySubmitted.value = false
    tab.value = 'studio'
    notice.value = '已填入原任务内容，确认后点击「开始生成」创建新任务。'
    await nextTick()
    document.getElementById('create-form')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  } catch (error) { formError.value = message(error) }
  finally { retryBusy.value = false }
}

async function askDelete(job: Job) {
  if (active(job)) return
  deleteTarget.value = job
  deleteError.value = ''
  await nextTick()
  deleteDialog.value?.showModal()
}

function cancelDelete() {
  if (deleting.value) return
  deleteDialog.value?.close()
  deleteTarget.value = null
}

async function confirmDelete() {
  if (!deleteTarget.value || deleting.value) return
  deleting.value = true
  try {
    const id = deleteTarget.value.id
    await request<void>(`/api/jobs/${encodeURIComponent(id)}`, { method: 'DELETE' })
    if (selected.value?.id === id) selected.value = null
    deleteDialog.value?.close()
    deleteTarget.value = null
    if (jobs.value.length === 1 && page.value > 1) page.value--
    else await loadHistory()
  } catch (error) { deleteError.value = message(error) }
  finally { deleting.value = false }
}

onMounted(() => { void initialize(); pollTimer = setInterval(() => { void poll() }, 4000) })
onBeforeUnmount(() => { if (pollTimer) clearInterval(pollTimer); if (previewURL.value) URL.revokeObjectURL(previewURL.value) })
</script>

<template>
  <div v-if="!ready" class="boot-screen" role="status"><span class="spinner"></span><span>正在打开工作台…</span></div>
  <div v-else-if="!authenticated" class="login-layout">
    <section class="login-story">
      <a class="brand" href="/" aria-label="Pic2Video 首页"><span class="brand-icon"><Icon name="play" :size="19" /></span><span>Pic2Video<span class="brand-dot">.</span></span></a>
      <div class="login-story-body">
        <span class="eyebrow">YOUR PRODUCT. IN MOTION.</span>
        <h1>让商品，<br>有了自己的故事<span>。</span></h1>
        <p>从一张好图片开始，<br>把商品亮点，变成看得见的短片。</p>
        <div class="story-art" aria-hidden="true"><div class="art-grid"></div><div class="art-photo"><Icon name="image" :size="52" /><small>01 / IMAGE</small></div><span class="art-arrow"><Icon name="arrow" :size="30" /></span><div class="art-video"><span class="art-rec"><i></i> PRODUCT STORY</span><div class="art-play"><Icon name="play" :size="35" /></div><small>00:05 <span>HD</span></small></div><div class="art-spark"><Icon name="spark" :size="30" /></div></div>
      </div>
      <div class="story-footer">一个轻量的商品视频工作台 <span>从图片到灵感，从灵感到视频</span></div>
    </section>
    <section class="login-panel">
      <form class="login-card" @submit.prevent="login">
        <span class="login-lock"><Icon name="lock" :size="24" /></span><span class="eyebrow">TEAM WORKSPACE</span>
        <h2>进入创作空间</h2><p>输入团队共享密码，开始制作商品视频。</p>
        <label for="password">共享密码</label>
        <input id="password" v-model="password" type="password" autocomplete="current-password" required placeholder="请输入共享密码" :disabled="loginBusy" autofocus>
        <p v-if="loginError" class="error-message" role="alert"><Icon name="info" :size="17" />{{ loginError }}</p>
        <button class="button primary login-submit" :disabled="loginBusy || !password"><span v-if="loginBusy" class="spinner"></span>{{ loginBusy ? '正在进入…' : '进入工作台' }}<Icon v-if="!loginBusy" name="arrow" :size="18" /></button>
        <div class="login-note"><Icon name="lock" :size="14" /> 团队共享空间 · 所有成员可查看任务历史</div>
      </form>
      <div class="login-copyright">Pic2Video / 商品视频工作台</div>
    </section>
  </div>
  <div v-else class="app-shell">
    <header class="topbar">
      <div class="topbar-inner">
        <a class="brand" href="/" aria-label="Pic2Video 首页"><span class="brand-icon"><Icon name="play" :size="19" /></span><span>Pic2Video<span class="brand-dot">.</span></span></a>
        <nav class="main-nav" aria-label="工作台导航"><button :class="{ active: tab === 'studio' }" @click="tab = 'studio'">视频工作台</button><button :class="{ active: tab === 'history' }" @click="tab = 'history'">创作记录 <span v-if="total">{{ total }}</span></button></nav>
        <div class="topbar-actions"><span class="mode-pill"><i></i>{{ provider === 'mock' ? '演示模式' : 'Seedance 模式' }}</span><button class="icon-button logout" title="退出登录" aria-label="退出登录" @click="logout"><Icon name="logout" :size="18" /></button></div>
      </div>
    </header>
    <main class="main-container">
      <div class="page-heading"><div><span class="eyebrow">{{ tab === 'studio' ? 'PRODUCT VIDEO STUDIO' : 'YOUR CREATIVE LIBRARY' }}</span><h1>{{ tab === 'studio' ? '让商品，动起来。' : '每一份灵感，都在这里。' }}</h1><p>{{ tab === 'studio' ? '上传一张商品图片，把你的产品故事变成短片。' : '团队共享的创作记录，随时回看、下载和整理。' }}</p></div><div class="heading-mark" aria-hidden="true"><Icon :name="tab === 'studio' ? 'spark' : 'film'" :size="35" /></div></div>
      <div v-if="provider === 'mock'" class="mode-banner"><span class="banner-icon"><Icon name="info" :size="18" /></span><p><strong>你正在体验演示模式</strong><span>返回固定 5 秒、360 × 640 样片；图片、时长和比例仅在真实生成模式生效。</span></p><span class="demo-label">MOCK</span></div>
      <p v-if="pollWarning" class="connection-warning" role="status"><Icon name="info" :size="16" />{{ pollWarning }} 正在自动重试。</p>

      <div v-show="tab === 'studio'" class="studio-grid">
        <section class="card create-card">
          <div class="section-heading"><div class="section-title"><span class="section-number">01</span><h2>准备商品素材</h2></div><span class="muted small">好素材，让故事更出彩</span></div>
          <form id="create-form" @submit.prevent="submit">
            <fieldset :disabled="submitting">
              <div class="field-heading"><label for="product-image">商品图片 <span class="required">*</span></label><span>单张图片</span></div>
              <input id="product-image" ref="imageInput" class="visually-hidden" type="file" accept="image/jpeg,image/png,image/webp" @change="acceptFile(($event.target as HTMLInputElement).files?.[0])">
              <button type="button" class="upload-zone" :class="{ dragging, 'has-image': file }" @click="imageInput?.click()" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="drop">
                <template v-if="file"><img :src="previewURL" alt="待生成商品图片预览"><span class="replace-image"><Icon name="upload" :size="15" /> 更换图片</span></template>
                <template v-else><span class="upload-icon"><Icon name="upload" :size="24" /></span><strong>点击上传，或将图片拖到这里</strong><span>JPG、PNG、WebP · 最大 10 MB</span></template>
              </button>
              <div v-if="file" class="file-info"><span><Icon name="check" :size="14" />{{ file.name }}</span><button type="button" class="text-button" aria-label="移除图片" @click="file = null"><Icon name="close" :size="14" /></button></div>
              <div class="field-heading description-heading"><label for="description">商品描述 <span class="required">*</span></label><span>{{ description.length }} / 2000</span></div>
              <textarea id="description" v-model="description" rows="4" maxlength="2000" required placeholder="这是什么商品？描述它的外观、核心卖点，以及你想呈现的画面。&#10;例如：一款磨砂白色保温杯，杯身简洁，适合日常通勤。用缓慢环绕镜头展示细节。"></textarea>
              <div class="settings-row"><div><label for="duration">视频时长</label><div class="select-wrap"><select id="duration" v-model.number="duration"><option :value="5">5 秒</option><option :value="10">10 秒</option></select><Icon name="chevron" :size="14" /></div></div><div><label for="ratio">画面比例</label><div class="select-wrap"><select id="ratio" v-model="ratio"><option value="9:16">9:16 · 竖屏</option><option value="16:9">16:9 · 横屏</option><option value="1:1">1:1 · 方形</option></select><Icon name="chevron" :size="14" /></div></div><div><label for="resolution">分辨率</label><div class="resolution" id="resolution">720p <span>HD</span></div></div></div>
              <p v-if="formError" class="error-message" role="alert"><Icon name="info" :size="16" />{{ formError }}</p>
              <p v-if="notice" class="success-message" role="status"><Icon name="check" :size="16" />{{ notice }}</p>
              <button type="submit" class="button primary generate-button" :disabled="!canSubmit"><span v-if="submitting" class="spinner"></span><Icon v-else :name="alreadySubmitted ? 'check' : 'spark'" :size="18" />{{ submitting ? '正在提交任务…' : alreadySubmitted ? '已提交任务' : '开始生成' }}<Icon v-if="!submitting && !alreadySubmitted" name="arrow" :size="18" /></button>
              <p class="form-footnote"><Icon name="lock" :size="13" /> 图片与视频保存在团队空间，关闭页面后任务继续</p>
            </fieldset>
          </form>
        </section>
        <section id="preview" class="card preview-card">
          <div class="section-heading"><div class="section-title"><span class="section-number">02</span><h2>视频预览</h2></div><span v-if="selected" class="status-badge" :class="selected.status"><i></i>{{ statusText(selected) }}</span><span v-else class="muted small">你的下一支短片</span></div>
          <div class="preview-stage" :class="{ 'has-video': selected?.status === 'succeeded' }">
            <template v-if="selected?.status === 'succeeded'"><video :key="selected.id" :src="jobVideo(selected)" controls playsinline preload="metadata" aria-label="生成视频预览"></video><span v-if="selected.provider === 'mock'" class="video-demo-tag">固定演示样片</span></template>
            <template v-else-if="selected"><img class="stage-background" :src="jobImage(selected)" alt=""><div class="stage-veil"></div><div class="stage-message"><span v-if="active(selected)" class="stage-symbol"><span class="spinner"></span></span><span v-else class="stage-symbol"><Icon name="info" :size="27" /></span><h3>{{ statusText(selected) }}</h3><p v-if="active(selected)">{{ selected.status === 'downloading' ? '正在将视频保存到团队空间。' : '画面正在酝酿，可以先去忙点别的。' }}</p><p v-else>{{ selected.error || '任务未完成，请检查后再试。' }}</p><small v-if="selected.provider === 'mock'">演示任务 · 固定样片</small></div></template>
            <template v-else><div class="empty-frame"><span class="frame-corner tl"></span><span class="frame-corner tr"></span><span class="frame-corner bl"></span><span class="frame-corner br"></span><div class="empty-play"><Icon name="play" :size="31" /></div><span class="frame-caption">WAITING FOR YOUR STORY</span></div><div class="empty-preview-copy"><h3>下一段故事，由你开始</h3><p>上传图片并提交，视频将在这里呈现</p></div><div class="stage-grid"></div></template>
          </div>
          <div v-if="selected" class="preview-details">
            <div class="preview-meta"><span>{{ selected.duration }} 秒 <b>·</b> {{ selected.ratio }} <b>·</b> {{ selected.resolution }}</span><span>{{ dateText(selected.created_at) }}</span></div>
            <div v-if="active(selected) || selected.status === 'succeeded'" class="task-steps" aria-label="任务状态"><div v-for="(step, index) in steps" :key="step" :class="{ done: selectedStep > index, current: selectedStep === index }"><span><Icon v-if="selectedStep > index" name="check" :size="12" /><template v-else>{{ index + 1 }}</template></span><small>{{ step }}</small></div></div>
            <div v-if="selected.status === 'succeeded'" class="preview-actions"><a class="button secondary" :href="`${jobVideo(selected)}?download=1`" download><Icon name="download" :size="17" />下载视频</a><button class="icon-button danger" aria-label="删除当前任务" title="删除任务" @click="askDelete(selected)"><Icon name="trash" :size="18" /></button></div>
            <button v-if="selected.status === 'failed'" class="button secondary retry-button" :disabled="retryBusy" @click="prepareRetry(selected)"><Icon name="refresh" :size="16" />{{ retryBusy ? '正在读取…' : '重新使用这些素材' }}</button>
            <p v-if="selected.status === 'unknown'" class="unknown-note">提交结果不确定，请先联系管理员确认远程任务，避免重复计费。</p>
          </div>
          <div v-else class="preview-tip"><Icon name="film" :size="20" /><p><strong>一张图片，就是一个起点</strong><span>清晰的主体、具体的描述，有助于呈现商品细节。</span></p></div>
        </section>
      </div>

      <section class="history-section" :class="{ 'history-only': tab === 'history' }">
        <div class="history-heading"><div><h2>{{ tab === 'studio' ? '最近的创作' : '全部创作' }}<span>{{ total }}</span></h2><p>团队共享记录，灵感随时回看</p></div><button class="button quiet" :disabled="historyLoading" @click="loadHistory"><Icon name="refresh" :size="16" />{{ historyLoading ? '刷新中' : '刷新记录' }}</button></div>
        <div v-if="historyError" class="error-message history-error" role="alert"><Icon name="info" :size="17" />{{ historyError }}</div>
        <div v-if="historyLoading && !jobs.length" class="history-empty" role="status"><span class="spinner"></span><p>正在读取创作记录…</p></div>
        <div v-else-if="!jobs.length" class="history-empty"><span class="empty-library"><Icon name="film" :size="29" /></span><h3>第一支短片，即将诞生</h3><p>提交商品素材后，你和团队的创作会出现在这里。</p><button v-if="tab === 'history'" class="text-button" @click="tab = 'studio'">开始第一次创作 <Icon name="arrow" :size="16" /></button></div>
        <div v-else class="history-grid"><article v-for="job in jobs" :key="job.id" class="job-card"><button class="job-thumbnail" :aria-label="`查看任务：${job.description}`" @click="openJob(job)"><img :src="jobImage(job)" alt="商品图片" loading="lazy"><span class="status-badge" :class="job.status"><i></i>{{ statusText(job) }}</span><span v-if="job.status === 'succeeded'" class="thumbnail-play"><Icon name="play" :size="21" /></span><span v-if="job.provider === 'mock'" class="thumbnail-demo">演示</span></button><div class="job-info"><button class="job-title" @click="openJob(job)">{{ job.description }}</button><div class="job-meta"><span>{{ job.duration }} 秒 · {{ job.ratio }} · {{ job.resolution }}</span><span>{{ dateText(job.created_at) }}</span></div><div class="job-actions"><button class="text-button" @click="openJob(job)">{{ job.status === 'succeeded' ? '查看视频' : '查看任务' }}<Icon name="arrow" :size="14" /></button><div><a v-if="job.status === 'succeeded'" class="icon-button" :href="`${jobVideo(job)}?download=1`" aria-label="下载视频" title="下载视频" download><Icon name="download" :size="16" /></a><button v-if="!active(job)" class="icon-button danger" aria-label="删除任务" title="删除任务" @click="askDelete(job)"><Icon name="trash" :size="16" /></button></div></div></div></article></div>
        <div v-if="pageCount > 1" class="pagination"><span>共 {{ total }} 个任务</span><div><button class="button quiet" :disabled="page === 1 || historyLoading" @click="page--">上一页</button><span>{{ page }} / {{ pageCount }}</span><button class="button quiet" :disabled="page === pageCount || historyLoading" @click="page++">下一页</button></div></div>
      </section>
      <footer class="page-footer"><span>Pic2Video<span class="brand-dot">.</span> <span class="footer-divider">/</span> 让每一件商品，都有好故事</span><span>团队工作空间 · {{ provider === 'mock' ? '演示模式' : 'Powered by Seedance' }}</span></footer>
    </main>
    <dialog ref="deleteDialog" class="delete-dialog" aria-labelledby="delete-title" @cancel.prevent="cancelDelete" @click="($event.target === deleteDialog) && cancelDelete()"><div class="dialog-body"><span class="delete-symbol"><Icon name="trash" :size="24" /></span><h2 id="delete-title">删除这条创作记录？</h2><p>关联的商品图片和视频也会被删除，团队成员将无法继续查看。此操作无法撤销。</p><p v-if="deleteTarget?.status === 'unknown'" class="unknown-note">这只删除本地记录，不会取消远程任务或退款。</p><p v-if="deleteError" class="error-message" role="alert">{{ deleteError }}</p><div class="dialog-actions"><button class="button secondary" :disabled="deleting" autofocus @click="cancelDelete">保留记录</button><button class="button danger-button" :disabled="deleting" @click="confirmDelete">{{ deleting ? '正在删除…' : '确认删除' }}</button></div></div></dialog>
  </div>
</template>
