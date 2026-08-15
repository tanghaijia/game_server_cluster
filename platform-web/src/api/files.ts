import { http } from './http'

export interface FileSession {
  base_url: string
  token: string
  instance_id: string
  data_root: string
  expires_at: string
}

export interface FileEntry {
  name: string
  is_dir: boolean
  size: number
  modified: string
}

// 用户：按订单获取会话（本人或管理员）；管理员：按实例获取会话
export function fetchUserSession(orderId: string): Promise<FileSession> {
  return http.post('/me/instances/' + orderId + '/file-session').then((r) => r.data)
}

export function fetchAdminSession(instanceId: string): Promise<FileSession> {
  return http.post('/admin/instances/' + instanceId + '/file-session').then((r) => r.data)
}

/**
 * FileClient：直连 node_agent 文件服务的客户端。
 * 会话（base_url + 短效 token）通过控制面获取；数据面操作全部直连，不经过 platform/controller。
 * 401/403（token 过期）时自动刷新会话并重试一次。
 */
export class FileClient {
  private session?: FileSession
  private ref: string
  private fetchSession: (ref: string) => Promise<FileSession>

  constructor(fetchSession: (ref: string) => Promise<FileSession>, ref: string) {
    this.fetchSession = fetchSession
    this.ref = ref
  }

  private base(s: FileSession): string {
    return s.base_url + '/v1/instances/' + s.instance_id
  }

  private async ensureSession(): Promise<FileSession> {
    if (!this.session || Date.parse(this.session.expires_at) - Date.now() < 60000) {
      this.session = await this.fetchSession(this.ref)
    }
    return this.session
  }

  // 通用直连请求；401/403 时刷新会话重试一次
  private async request<T>(fn: (s: FileSession) => Promise<T>): Promise<T> {
    const s = await this.ensureSession()
    try {
      return await fn(s)
    } catch (e: any) {
      const status = e?.response?.status ?? e?.status
      if (status === 401 || status === 403) {
        this.session = undefined
        const s2 = await this.ensureSession()
        return await fn(s2)
      }
      throw e
    }
  }

  async list(path: string): Promise<{ path: string; entries: FileEntry[] }> {
    return this.request(async (s) => {
      const params = new URLSearchParams({ path })
      const resp = await fetch(this.base(s) + '/files?' + params, {
        headers: { Authorization: 'Bearer ' + s.token },
      })
      return handleResp(resp)
    })
  }

  async mkdir(path: string): Promise<void> {
    return this.request(async (s) => {
      const resp = await fetch(this.base(s) + '/files/mkdir?path=' + encodeURIComponent(path), {
        method: 'POST',
        headers: { Authorization: 'Bearer ' + s.token },
      })
      return handleResp(resp)
    })
  }

  async rename(from: string, to: string): Promise<void> {
    return this.request(async (s) => {
      const resp = await fetch(
        this.base(s) + '/files/rename?from=' + encodeURIComponent(from) + '&to=' + encodeURIComponent(to),
        { method: 'POST', headers: { Authorization: 'Bearer ' + s.token } },
      )
      return handleResp(resp)
    })
  }

  async del(path: string): Promise<void> {
    return this.request(async (s) => {
      const resp = await fetch(this.base(s) + '/files?path=' + encodeURIComponent(path), {
        method: 'DELETE',
        headers: { Authorization: 'Bearer ' + s.token },
      })
      return handleResp(resp)
    })
  }

  async readText(path: string): Promise<string> {
    return this.request(async (s) => {
      const resp = await fetch(this.base(s) + '/files/text?path=' + encodeURIComponent(path), {
        headers: { Authorization: 'Bearer ' + s.token },
      })
      const data = await handleResp(resp)
      return data.content
    })
  }

  async writeText(path: string, content: string): Promise<void> {
    return this.request(async (s) => {
      const resp = await fetch(this.base(s) + '/files/text?path=' + encodeURIComponent(path), {
        method: 'PUT',
        headers: { Authorization: 'Bearer ' + s.token, 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      })
      return handleResp(resp)
    })
  }

  // 上传（XHR 以支持进度条）；401 由调用方处理（此处简单抛错）
  async upload(path: string, file: File, onProgress?: (pct: number) => void): Promise<void> {
    const s = await this.ensureSession()
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('PUT', this.base(s) + '/files/content?path=' + encodeURIComponent(path))
      xhr.setRequestHeader('Authorization', 'Bearer ' + s.token)
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100))
      }
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          onProgress?.(100)
          resolve()
        } else if (xhr.status === 401 || xhr.status === 403) {
          reject({ status: xhr.status })
        } else {
          reject(new Error('上传失败: HTTP ' + xhr.status))
        }
      }
      xhr.onerror = () => reject(new Error('上传失败（网络错误）'))
      xhr.send(file)
    })
  }

  // 下载地址（<a> 直接下载；token 走 query，见 node_agent download 支持 ?token=）
  async downloadUrl(path: string): Promise<string> {
    const s = await this.ensureSession()
    return this.base(s) + '/files/content?path=' + encodeURIComponent(path) + '&token=' + encodeURIComponent(s.token)
  }
}

async function handleResp(resp: Response): Promise<any> {
  if (resp.status === 401 || resp.status === 403) {
    throw { status: resp.status }
  }
  if (!resp.ok) {
    let msg = 'HTTP ' + resp.status
    try {
      const data = await resp.json()
      if (data?.error) msg = data.error
    } catch {
      /* ignore */
    }
    throw new Error(msg)
  }
  return resp.json()
}
