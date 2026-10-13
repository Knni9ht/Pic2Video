export type Provider = 'mock' | 'seedance'
export type Status = 'queued' | 'submitting' | 'running' | 'downloading' | 'succeeded' | 'failed' | 'unknown'

export interface Job {
  id: string
  description: string
  duration: number
  ratio: string
  resolution: string
  status: Status
  provider: Provider
  created_at: string
  updated_at: string
  error: string
  image_url: string
  video_url: string
}

export interface Session { authenticated: boolean; provider: Provider }
export interface JobPage { items: Job[]; total: number; page: number; page_size: number }

export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message) }
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, { ...options, credentials: 'same-origin' })
  } catch {
    throw new APIError(0, 'network_error', '暂时无法连接服务，请检查网络后重试。')
  }
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new APIError(response.status, body?.error?.code ?? 'request_failed', body?.error?.message ?? '操作未完成，请稍后重试。')
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const jobImage = (job: Job) => `/api/jobs/${encodeURIComponent(job.id)}/image`
export const jobVideo = (job: Job) => `/api/jobs/${encodeURIComponent(job.id)}/video`

export function newRequestKey(): string {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6]! & 0x0f) | 0x40
  bytes[8] = (bytes[8]! & 0x3f) | 0x80
  const hex = [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
