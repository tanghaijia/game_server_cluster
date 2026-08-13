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

export async function updateBranchCache(gameId: string, branch: string, nodeAgentId: string): Promise<void> {
  await http.post('/admin/games/' + gameId + '/branches/' + branch + '/cache', { node_agent_id: nodeAgentId })
}
