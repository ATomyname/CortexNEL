import * as binding from '../../wailsjs/go/app/App'
import { app } from '../../wailsjs/go/models'
export type RentalServer = app.RentalServerDTO
export function listRentalServers(): Promise<RentalServer[]> { return binding.ListRentalServers() }
export function getRentalServerDetail(id: string): Promise<RentalServer> { return binding.GetRentalServerDetail(id) }
export type RentalRole = app.RoleDTO
export function listRentalRoles(id: string): Promise<RentalRole[]> { return binding.ListRentalRoles(id) }
export function createRentalRole(id: string, name: string): Promise<void> { return binding.CreateRentalRole(id, name) }
export function resolveRentalServerAddress(id: string, password: string): Promise<app.NetworkAddressDTO> { return binding.ResolveRentalServerAddress(id, password) }
