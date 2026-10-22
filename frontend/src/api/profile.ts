// 档案域薄封装（架构文档 §10.2）。
import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'

export type Server = app.ServerDTO
export type Env = app.EnvDTO

export function listServers(): Promise<Server[]> {
  return binding.ListServers()
}

export function saveServer(server: Server): Promise<void> {
  return binding.SaveServer(server)
}

export function removeServer(id: string): Promise<void> { return binding.RemoveServer(id) }

export function listEnvs(): Promise<Env[]> {
  return binding.ListEnvs()
}

export function saveEnv(env: Env): Promise<void> {
  return binding.SaveEnv(env)
}

export function removeEnv(id: string): Promise<void> { return binding.RemoveEnv(id) }

export function detectJava(): Promise<string> { return binding.DetectJava() }
