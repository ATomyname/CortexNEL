// 网络服域薄封装（架构文档 §10.2）：实时网易网络服列表 / 详情 / 地址。
import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'

export type NetworkServer = app.NetworkServerDTO
export type NetworkServerDetail = app.NetworkServerDetailDTO
export type NetworkAddress = app.NetworkAddressDTO
export type Role = app.RoleDTO

export function listNetworkServers(): Promise<NetworkServer[]> {
  return binding.ListNetworkServers()
}

export function getNetworkServerDetail(entityId: string): Promise<NetworkServerDetail> {
  return binding.GetNetworkServerDetail(entityId)
}

export function resolveNetworkServerAddress(entityId: string): Promise<NetworkAddress> {
  return binding.ResolveNetworkServerAddress(entityId)
}

export function listRoles(entityId: string): Promise<Role[]> {
  return binding.ListNetworkServerRoles(entityId)
}

export function createRole(entityId: string, name: string): Promise<void> {
  return binding.CreateNetworkServerRole(entityId, name)
}
