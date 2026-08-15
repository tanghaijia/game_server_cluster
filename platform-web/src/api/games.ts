import { http } from './http'

export interface GameProfile {
  game_id: string
  display_name: string
  icon_url: string
  accent_color: string
  description: string
  enabled: boolean
  sort_order: number
}

export interface GameView {
  ID: string
  Name: string
  AppId: string
  ContainerConfigID: string
  profile?: GameProfile
}

export async function listGames(): Promise<GameView[]> {
  const resp = await http.get('/games')
  return resp.data.games
}

export async function getGame(gameId: string): Promise<GameView> {
  const resp = await http.get('/games/' + encodeURIComponent(gameId))
  return resp.data
}
