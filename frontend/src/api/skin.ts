import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'

export type Skin = app.SkinDTO
export function listSkins(keyword = ''): Promise<Skin[]> { return binding.ListSkins(keyword) }
export function applySkin(entityId: string): Promise<void> { return binding.ApplySkin(entityId) }
export function getCurrentSkin(): Promise<Skin> { return binding.GetCurrentSkin() }
export function ensureRandomSkinIfNone(): Promise<Skin> { return binding.EnsureRandomSkinIfNone() }
export function randomizeSkin(): Promise<Skin> { return binding.RandomizeSkin() }
