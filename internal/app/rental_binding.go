package app

import (
	"fmt"
	"nexus/internal/netease"
)

func (a *App) ListRentalServers() ([]RentalServerDTO, error) {
	var items []netease.RentalServer
	err := a.withGateway(func(gw *netease.Gateway) error {
		var gErr error
		items, gErr = gw.ListRentalServers(a.ctx, 0, 50)
		return gErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]RentalServerDTO, 0, len(items))
	for _, item := range items {
		out = append(out, rentalDTO(item))
	}
	return out, nil
}

func (a *App) GetRentalServerDetail(entityID string) (RentalServerDTO, error) {
	if entityID == "" {
		return RentalServerDTO{}, fmt.Errorf("租赁服 ID 不能为空")
	}
	var item netease.RentalServer
	err := a.withGateway(func(gw *netease.Gateway) error {
		var gErr error
		item, gErr = gw.RentalDetail(a.ctx, entityID)
		return gErr
	})
	if err != nil {
		return RentalServerDTO{}, err
	}
	return rentalDTO(item), nil
}

func (a *App) ListRentalRoles(entityID string) ([]RoleDTO, error) {
	if entityID == "" {
		return nil, fmt.Errorf("租赁服 ID 不能为空")
	}
	var roles []netease.RentalRole
	err := a.withGateway(func(gw *netease.Gateway) error {
		var gErr error
		roles, gErr = gw.ListRentalRoles(a.ctx, entityID)
		return gErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]RoleDTO, 0, len(roles))
	for _, role := range roles {
		out = append(out, RoleDTO{ID: role.ID, Name: role.Name})
	}
	return out, nil
}

func (a *App) CreateRentalRole(entityID, name string) error {
	if entityID == "" || name == "" {
		return fmt.Errorf("租赁服 ID / 角色名不能为空")
	}
	return a.withGateway(func(gw *netease.Gateway) error {
		acct, err := a.accounts.Get(a.settings.MainAccountID)
		if err != nil {
			return err
		}
		return gw.CreateRentalRole(a.ctx, entityID, name, acct.UserID)
	})
}

func (a *App) ResolveRentalServerAddress(entityID, password string) (NetworkAddressDTO, error) {
	if entityID == "" {
		return NetworkAddressDTO{}, fmt.Errorf("租赁服 ID 不能为空")
	}
	var addr *netease.NetGameAddress
	err := a.withGateway(func(gw *netease.Gateway) error {
		var gErr error
		addr, gErr = gw.RentalAddress(a.ctx, entityID, password)
		return gErr
	})
	if err != nil {
		return NetworkAddressDTO{}, err
	}
	return NetworkAddressDTO{Host: addr.IP, Port: addr.Port}, nil
}

func rentalDTO(item netease.RentalServer) RentalServerDTO {
	return RentalServerDTO{EntityID: item.EntityID, Name: item.Name, ServerName: item.ServerName, HasPassword: item.HasPassword, McVersion: item.McVersion, PlayerCount: item.PlayerCount, Capacity: item.Capacity, ImageURL: item.ImageURL, BriefSummary: item.BriefSummary}
}
