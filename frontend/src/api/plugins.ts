import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'
export type Plugin = app.PluginDTO
export type EnvInfo = app.EnvDTO
export function listPlugins(): Promise<Plugin[]> { return binding.ListPlugins() }
export function togglePlugin(fileName: string): Promise<void> { return binding.TogglePlugin(fileName) }
export function openModsDir(): Promise<void> { return binding.OpenModsDir() }
export function getClientEnvInfo(): Promise<EnvInfo> { return binding.GetClientEnvInfo() }
