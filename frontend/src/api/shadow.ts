// 影子端域薄封装（架构文档 §10.2）。绑定在 internal/app/shadow_binding.go。
import * as binding from '../../wailsjs/go/app/App'

export interface ShadowGateStats {
  swallowedA: number
  swallowedB: number
  fallbacks: number
  blockedSB: number
  blockedCB: number
}

export interface ShadowStatus {
  enabled: boolean
  listening: boolean
  addr: string
  stage: string
  sender: string
  gateStats: ShadowGateStats
  recorderFrames: number
  recorderBytes: number
  recorderReady: boolean
  truncated: boolean
}

export function startShadow(port: number): Promise<ShadowStatus> {
  const b = binding as unknown as {
    StartShadow?: (port: number) => Promise<ShadowStatus>
  }
  if (typeof b.StartShadow === 'function') {
    return b.StartShadow(port)
  }
  return Promise.reject('StartShadow 绑定未生成（需重新 wails generate）')
}

export function stopShadow(): Promise<ShadowStatus> {
  const b = binding as unknown as { StopShadow?: () => Promise<ShadowStatus> }
  if (typeof b.StopShadow === 'function') {
    return b.StopShadow()
  }
  return Promise.reject('StopShadow 绑定未生成')
}

export function getShadowStatus(): Promise<ShadowStatus> {
  const b = binding as unknown as { ShadowStatus?: () => Promise<ShadowStatus> }
  if (typeof b.ShadowStatus === 'function') {
    return b.ShadowStatus()
  }
  return Promise.reject('ShadowStatus 绑定未生成')
}

export function setShadowSender(sender: 'A' | 'B'): Promise<ShadowStatus> {
  const b = binding as unknown as { SetShadowSender?: (sender: string) => Promise<ShadowStatus> }
  if (typeof b.SetShadowSender === 'function') {
    return b.SetShadowSender(sender)
  }
  return Promise.reject('SetShadowSender 绑定未生成')
}
