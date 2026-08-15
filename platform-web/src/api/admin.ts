import { http } from './http'

// ---- 类型（与 controller 返回的 PascalCase JSON 一致） ----

export interface Game {
  ID: string
  Name: string
  AppId: string
  ContainerConfigID: string
}

export interface Node {
  Id: number
  Ip: string
  CoreNum: number
  CoreFrequency: number
  MemorySize: number
  StorageSize: number
  Location: string
  ServiceProvider: string
}

export interface NodeAgent {
  ID: string
  NodeId: string
  Port: number
  Status: number // 0=Disabled 1=Enabled
  Alive: boolean // 存活检测结果（controller 心跳探测）
  LastHeartbeatAt?: string
}

export interface SteamBranch {
  Id: string
  BranchName: string
  LastBuildId: number
  Description: string
  GameId: string
  Status: number // 0=Disable 1=Enable 2=Abandoned
  CreateTime: string
  UpdateTime: string
}

// ---- Node ----

export async function listNodes(): Promise<Node[]> {
  const resp = await http.get('/admin/nodes')
  return resp.data.nodes
}

export async function createNode(ip: string): Promise<Node> {
  const resp = await http.post('/admin/nodes', { ip })
  return resp.data
}

// ---- NodeAgent ----

export async function listNodeAgents(): Promise<NodeAgent[]> {
  const resp = await http.get('/admin/node-agents')
  return resp.data.node_agents
}

export async function createNodeAgent(data: { name: string; node_id?: string; port?: number }): Promise<NodeAgent> {
  const resp = await http.post('/admin/node-agents', data)
  return resp.data
}

export async function setNodeAgentEnabled(id: string, enabled: boolean): Promise<NodeAgent> {
  const resp = await http.post('/admin/node-agents/' + id + (enabled ? '/enable' : '/disable'))
  return resp.data
}

// ---- Game ----

export async function listGames(): Promise<Game[]> {
  const resp = await http.get('/admin/games')
  return resp.data.games
}

export async function createGame(data: { name: string; app_id?: string }): Promise<Game> {
  const resp = await http.post('/admin/games', data)
  return resp.data
}

export async function updateGame(id: string, data: { name: string; app_id?: string }): Promise<Game> {
  const resp = await http.put('/admin/games/' + id, data)
  return resp.data
}

export async function deleteGame(id: string): Promise<void> {
  await http.delete('/admin/games/' + id)
}

// ---- SteamBranch ----

export async function listBranches(gameId: string): Promise<SteamBranch[]> {
  const resp = await http.get('/admin/games/' + gameId + '/branches')
  return resp.data.branches
}

export async function syncBranches(gameId: string): Promise<void> {
  await http.post('/admin/games/' + gameId + '/branches/sync')
}

// ---- 游戏资料（多游戏平台） ----

export interface GameProfileInput {
  display_name?: string
  icon_url?: string
  accent_color?: string
  description?: string
  enabled?: boolean
  sort_order?: number
}

export async function updateGameProfile(gameId: string, data: GameProfileInput) {
  const resp = await http.put('/admin/games/' + gameId + '/profile', data)
  return resp.data
}

export async function uploadGameIcon(gameId: string, file: File): Promise<{ icon_url: string }> {
  const form = new FormData()
  form.append('file', file)
  const resp = await http.post('/admin/games/' + gameId + '/icon', form)
  return resp.data
}

// ---- game_build（资产版本） ----

export interface GameBuild {
  build_id: string
  game?: { id: string }
  channel?: string
  adapter_id?: string
  adapter_version?: string
  upstream_version?: string
  artifact_uri?: string
  artifact_image_name?: string
  artifact_image_tag?: string
  status?: number
  created_at?: string
  updated_at?: string
}

export const BUILD_STATUS = ['unknown', 'discovered', 'resolving', 'available', 'deprecated', 'unavailable', 'deleted']

export async function listGameBuilds(gameId: string, channel?: string): Promise<GameBuild[]> {
  const resp = await http.get('/admin/games/' + gameId + '/builds', { params: channel ? { channel } : {} })
  return resp.data.builds
}

export async function registerGameBuild(gameId: string, data: Record<string, string>): Promise<GameBuild> {
  const resp = await http.post('/admin/games/' + gameId + '/builds', data)
  return resp.data
}

export async function updateBranchCache(gameId: string, branch: string, nodeAgentId: string): Promise<void> {
  await http.post('/admin/games/' + gameId + '/branches/' + branch + '/cache', { node_agent_id: nodeAgentId })
}
