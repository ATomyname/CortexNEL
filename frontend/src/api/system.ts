// 系统域薄封装（架构文档 §10.2）。
import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'

export type Settings = app.SettingsDTO
export type GenerateRoleNameOptions = app.GenerateRoleNameOptions

export function getSettings(): Promise<Settings> {
  return binding.GetSettings()
}

export function saveSettings(settings: Settings): Promise<void> {
  return binding.SaveSettings(settings)
}

export function saveCredentialOptIn(enabled: boolean): Promise<void> {
  return binding.SaveCredentialOptIn(enabled)
}

export function openDataDir(): Promise<void> {
  return binding.OpenDataDir()
}

export function generateRoleName(opts: Partial<app.GenerateRoleNameOptions>): Promise<string> {
  return binding.GenerateRoleName(opts as app.GenerateRoleNameOptions)
}

export function batchGenerateRoleNames(opts: Partial<app.GenerateRoleNameOptions>, count: number): Promise<string[]> {
  return binding.BatchGenerateRoleNames(opts as app.GenerateRoleNameOptions, count)
}

export function convertHomophone(text: string, exactTone = false, allowSelf = true): Promise<string> {
  return binding.ConvertHomophone(text, exactTone, allowSelf)
}

export function batchConvertHomophones(text: string, count = 5, exactTone = false, allowSelf = true): Promise<string[]> {
  return binding.BatchConvertHomophones(text, count, exactTone, allowSelf)
}

export function getLocalIPv4s(): Promise<string[]> {
  return binding.GetLocalIPv4s()
}

export function configureFirewall(port = 25565): Promise<void> {
  return binding.ConfigureFirewall(port)
}
