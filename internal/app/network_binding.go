// 网络服域绑定（架构文档 §10.1）：实时网易网络服列表 / 详情 / 地址解析。
// 数据来源：x19 网关（facts §5.1/§5.3），凭据取主账号（与进服链路一致）。
package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"nexus/internal/account"
	"nexus/internal/netease"
)

// mainAccountToken 返回主账号与有效 token（未登录或凭据过期时给可读错误，
// 过期时用保存的凭据自动重登）。
func (a *App) mainAccountToken() (account.Account, string, error) {
	accountID := a.settings.MainAccountID
	if accountID == "" {
		list, _ := a.accounts.List()
		if len(list) > 0 {
			accountID = list[0].ID
			a.settings.MainAccountID = accountID
			_ = a.saveSettings()
		}
	}
	if accountID == "" {
		return account.Account{}, "", fmt.Errorf("未登录游戏账号：请先在「账号管理」添加账号")
	}
	acct, err := a.accounts.Get(accountID)
	if err != nil {
		return account.Account{}, "", fmt.Errorf("主账号不存在")
	}
	token, err := a.accounts.Token(accountID)
	if err != nil || token == "" {
		// 尝试使用保存的凭据自动重新登录
		if acct.Username != "" && acct.EncryptedPassword != "" {
			if newAcct, newToken, reErr := a.logins.ReLogin(a.ctx, accountID); reErr == nil && newToken != "" {
				acct = newAcct
				token = newToken
				err = nil
			}
		}
		if err != nil || token == "" {
			return account.Account{}, "", fmt.Errorf("账号凭据已过期或在重启后失效，请在「账号管理」中重新登录: %w", err)
		}
	}
	return acct, token, nil
}

// mainGateway 用主账号凭据构造网关客户端。
func (a *App) mainGateway() (*netease.Gateway, error) {
	acct, token, err := a.mainAccountToken()
	if err != nil {
		return nil, err
	}
	return netease.NewGateway(netease.Credentials{UserID: acct.UserID, Token: token}), nil
}

// withGateway 包装网关调用：当返回 Token 过期（code=10 / 请先登录等）时，自动使用保存的凭据静默重登并重试一次。
func (a *App) withGateway(fn func(gw *netease.Gateway) error) error {
	gw, err := a.mainGateway()
	if err != nil {
		return err
	}
	err = fn(gw)
	if err != nil && netease.IsTokenExpired(err) {
		accountID := a.settings.MainAccountID
		if accountID != "" {
			if _, newToken, reErr := a.logins.ReLogin(a.ctx, accountID); reErr == nil && newToken != "" {
				if newGw, gwErr := a.mainGateway(); gwErr == nil {
					return fn(newGw)
				}
			}
		}
	}
	return err
}

// ListNetworkServers 分页拉取全部网络服（网易单页上限约 100），并并发补全封面大图。
func (a *App) ListNetworkServers() ([]NetworkServerDTO, error) {
	var allItems []netease.NetGameItem
	var activeGw *netease.Gateway

	err := a.withGateway(func(gw *netease.Gateway) error {
		activeGw = gw
		allItems = nil
		const pageSize = 100
		for offset := 0; ; offset += pageSize {
			items, err := gw.ListNetGames(a.ctx, offset, pageSize)
			if err != nil {
				return err
			}
			allItems = append(allItems, items...)
			if len(items) < pageSize {
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 收集未命中封面或版本缓存的服务器 ID
	var missingIDs []string
	for _, it := range allItems {
		_, hasImg := a.serverImages.Load(it.EntityID)
		_, hasVer := a.serverVersions.Load(it.EntityID)
		if !hasImg || !hasVer {
			missingIDs = append(missingIDs, it.EntityID)
		}
	}

	// 最多 8 并发拉取详情补齐封面图与协议版本（超时 3 秒兜底，不阻断主列表）
	if len(missingIDs) > 0 && activeGw != nil {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, id := range missingIDs {
			wg.Add(1)
			sem <- struct{}{}
			go func(entityID string) {
				defer wg.Done()
				defer func() { <-sem }()
				ctx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
				defer cancel()
				if d, err := activeGw.NetGameDetail(ctx, entityID); err == nil {
					if len(d.BriefImageURLs) > 0 {
						a.serverImages.Store(entityID, d.BriefImageURLs[0])
					}
					hasJava := false
					isBedrock := false
					mainVer := ""
					for _, v := range d.McVersionList {
						if v.Name != "" {
							if strings.HasPrefix(v.Name, "1.") {
								hasJava = true
								if mainVer == "" {
									mainVer = v.Name
								}
							} else if strings.HasPrefix(v.Name, "100.") {
								isBedrock = true
							}
						}
					}
					if !hasJava && isBedrock {
						mainVer = "基岩版"
						a.serverBedrock.Store(entityID, true)
					} else {
						a.serverBedrock.Store(entityID, false)
					}
					if mainVer == "" {
						mainVer = "1.20"
					}
					a.serverVersions.Store(entityID, mainVer)

					isMaint := d.Status == 2 || d.ServerStatus == 2 || d.IsMaintenance ||
						(d.ServerAddress == "0.0.0.0" && d.ServerPort == 0) ||
						strings.Contains(d.DetailDescription, "维护中") ||
						strings.Contains(d.DetailDescription, "停服维护")
					a.serverMaintaining.Store(entityID, isMaint)
				}
			}(id)
		}
		wg.Wait()
	}

	out := make([]NetworkServerDTO, 0, len(allItems))
	for _, it := range allItems {
		img := ""
		if val, ok := a.serverImages.Load(it.EntityID); ok {
			img, _ = val.(string)
		}
		if img == "" {
			img = it.ImageURL
		}
		if img == "" {
			img = it.TitleImageURL
		}
		if img == "" && len(it.BriefImageURLs) > 0 {
			img = it.BriefImageURLs[0]
		}
		ver := ""
		if val, ok := a.serverVersions.Load(it.EntityID); ok {
			ver, _ = val.(string)
		}
		if ver == "" {
			ver = "1.20"
		}
		isBedrock := false
		if val, ok := a.serverBedrock.Load(it.EntityID); ok {
			isBedrock, _ = val.(bool)
		}
		isMaint := false
		if val, ok := a.serverMaintaining.Load(it.EntityID); ok {
			isMaint, _ = val.(bool)
		}
		out = append(out, NetworkServerDTO{
			EntityID:      it.EntityID,
			Name:          it.Name,
			OnlineCount:   it.OnlineCount,
			BriefSummary:  it.BriefSummary,
			ImageURL:      img,
			McVersion:     ver,
			IsBedrockOnly: isBedrock,
			IsMaintaining: isMaint,
		})
	}
	return out, nil
}

// GetNetworkServerDetail 拉取网络服详情（封面图/简介/版本/维护状态/基岩标识）。
func (a *App) GetNetworkServerDetail(entityID string) (NetworkServerDetailDTO, error) {
	if entityID == "" {
		return NetworkServerDetailDTO{}, fmt.Errorf("entityId 不能为空")
	}
	var d *netease.NetGameDetail
	err := a.withGateway(func(gw *netease.Gateway) error {
		var gErr error
		d, gErr = gw.NetGameDetail(a.ctx, entityID)
		return gErr
	})
	if err != nil {
		return NetworkServerDetailDTO{}, err
	}
	image := ""
	if len(d.BriefImageURLs) > 0 {
		image = d.BriefImageURLs[0]
	}
	var versionList []string
	hasJava := false
	isBedrock := false
	mainVersion := ""
	for _, v := range d.McVersionList {
		if v.Name != "" {
			versionList = append(versionList, v.Name)
			// 优先挑选以 "1." 开头的标准 Java 版版本号（如 1.20, 1.21.8, 1.12.2），跳过 100.0.0 (Apollo/Bedrock)
			if strings.HasPrefix(v.Name, "1.") {
				hasJava = true
				if mainVersion == "" || strings.HasPrefix(mainVersion, "100.") {
					mainVersion = v.Name
				}
			} else if strings.HasPrefix(v.Name, "100.") {
				isBedrock = true
			} else if mainVersion == "" {
				mainVersion = v.Name
			}
		}
	}
	isBedrockOnly := !hasJava && isBedrock
	if isBedrockOnly {
		mainVersion = "基岩版"
	}
	if mainVersion == "" || strings.HasPrefix(mainVersion, "100.") {
		mainVersion = "1.20"
	}
	isMaint := d.Status == 2 || d.ServerStatus == 2 || d.IsMaintenance ||
		(d.ServerAddress == "0.0.0.0" && d.ServerPort == 0) ||
		strings.Contains(d.DetailDescription, "维护中") ||
		strings.Contains(d.DetailDescription, "停服维护")

	return NetworkServerDetailDTO{
		EntityID:      d.EntityID,
		Name:          d.Name,
		ImageURL:      image,
		Description:   d.DetailDescription,
		McVersion:     mainVersion,
		McVersionList: versionList,
		IsBedrockOnly: isBedrockOnly,
		IsMaintaining: isMaint,
	}, nil
}

// ResolveNetworkServerAddress 解析网络服上游地址（存档案/进服用）。
func (a *App) ResolveNetworkServerAddress(entityID string) (NetworkAddressDTO, error) {
	if entityID == "" {
		return NetworkAddressDTO{}, fmt.Errorf("entityId 不能为空")
	}
	var addr *netease.NetGameAddress
	err := a.withGateway(func(gw *netease.Gateway) error {
		var gErr error
		addr, gErr = gw.NetGameAddress(a.ctx, entityID)
		return gErr
	})
	if err != nil {
		return NetworkAddressDTO{}, err
	}
	if addr.IP == "" || addr.Port == 0 {
		return NetworkAddressDTO{}, fmt.Errorf("网络服 %s 未返回有效地址", entityID)
	}
	return NetworkAddressDTO{Host: addr.IP, Port: addr.Port}, nil
}

func (a *App) ListNetworkServerRoles(entityID string) ([]RoleDTO, error) {
	if entityID == "" {
		return nil, fmt.Errorf("entityId 不能为空")
	}
	var roles []netease.GameCharacter
	err := a.withGateway(func(gw *netease.Gateway) error {
		var gErr error
		roles, gErr = gw.QueryRoles(a.ctx, entityID, 0, 50)
		return gErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]RoleDTO, 0, len(roles))
	for _, role := range roles {
		if role.Name != "" {
			out = append(out, RoleDTO{ID: role.ID, Name: role.Name})
		}
	}
	return out, nil
}

func (a *App) CreateNetworkServerRole(entityID, name string) error {
	if entityID == "" || name == "" {
		return fmt.Errorf("服务器 ID / 角色名不能为空")
	}
	return a.withGateway(func(gw *netease.Gateway) error {
		return gw.CreateRole(a.ctx, entityID, name)
	})
}
