import { http } from './http'

/**
 * node_agent 日志读取（P2，见 docs/node-agent-logging-design.md）。
 * 会话（base_url + 短效 token）经控制面签发；日志数据面直连 node_agent，不经过 platform/controller。
 * 401/403（token 过期）自动刷新会话并重试一次。
 */

export interface AgentLogSession {
  base_url: string
  token: string
  agent_id: string
  expires_at: string
}

export interface AgentLogTail {
  text: string
  offset: number
  truncated: boolean
  rotated: boolean
  error?: string
}

// 管理员：为指定 node_agent 签发日志会话（POST /api/admin/node-agents/:id/log-session）
export function fetchAgentLogSession(agentId: string): Promise<AgentLogSession> {
  return http.post('/admin/node-agents/' + agentId + '/log-session').then((r) => r.data)
}

/**
 * AgentLogClient：直连 node_agent 日志端点的客户端。
 */
export class AgentLogClient {
  private session?: AgentLogSession
  private agentId: string

  constructor(agentId: string) {
    this.agentId = agentId
  }

  private async ensureSession(): Promise<AgentLogSession> {
    if (!this.session || Date.parse(this.session.expires_at) - Date.now() < 60000) {
      this.session = await fetchAgentLogSession(this.agentId)
    }
    return this.session
  }

  // 通用直连请求；401/403 时刷新会话重试一次
  private async request<T>(fn: (s: AgentLogSession) => Promise<T>): Promise<T> {
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

  /**
   * tail 日志。
   * @param lines 最大行数（默认 300）
   * @param level info/warn/error，空 = 全部
   * @param keyword 关键词子串（大小写不敏感）
   * @param offset 增量游标；无 = 尾部窗口
   */
  async tail(lines: number, level: string, keyword: string, offset?: number): Promise<AgentLogTail> {
    return this.request(async (s) => {
      const params = new URLSearchParams({ lines: String(lines) })
      if (level) params.set('level', level)
      if (keyword) params.set('keyword', keyword)
      if (offset != null) params.set('offset', String(offset))
      const resp = await fetch(s.base_url + '/v1/agent/logs/tail?' + params, {
        headers: { Authorization: 'Bearer ' + s.token },
      })
      return handleResp(resp)
    })
  }
}

async function handleResp(resp: Response): Promise<AgentLogTail> {
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
  const data = await resp.json()
  if (data?.error) throw new Error(data.error) // 200 但业务错误（如日志未启用）
  return data
}
