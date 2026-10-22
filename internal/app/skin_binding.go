package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"nexus/internal/netease"
)

func (a *App) ListSkins(keyword string) ([]SkinDTO, error) {
	var items []netease.SkinItem
	var activeGw *netease.Gateway
	err := a.withGateway(func(gw *netease.Gateway) error {
		activeGw = gw
		var gErr error
		if keyword == "" {
			items, gErr = gw.ListFreeSkins(a.ctx, 0, 40)
		} else {
			items, gErr = gw.SearchSkins(a.ctx, keyword, 0, 40)
		}
		return gErr
	})
	if err != nil {
		return nil, err
	}

	// 异步检查并自动为未设皮肤的账号随机挑选并应用一套皮肤
	go func() {
		_, _ = a.EnsureRandomSkinIfNone()
	}()

	// 收集未命中缓存的皮肤 ID
	var missingIDs []string
	for _, it := range items {
		if _, ok := a.skinImages.Load(it.EntityID); !ok {
			missingIDs = append(missingIDs, it.EntityID)
		}
	}

	// 步骤 1：使用网易 search-by-ids 批量拉取图片（单次请求搞定全列表）
	if len(missingIDs) > 0 && activeGw != nil {
		if batchImgs, err := activeGw.BatchSkinImages(a.ctx, missingIDs); err == nil && len(batchImgs) > 0 {
			for id, img := range batchImgs {
				a.skinImages.Store(id, img)
			}
		}
	}

	// 步骤 2：对仍未命中的个别皮肤，并发拉取详情（最多 8 并发，3 秒超时兜底）
	var stillMissing []string
	for _, it := range items {
		if _, ok := a.skinImages.Load(it.EntityID); !ok {
			stillMissing = append(stillMissing, it.EntityID)
		}
	}
	if len(stillMissing) > 0 && activeGw != nil {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, id := range stillMissing {
			wg.Add(1)
			sem <- struct{}{}
			go func(entityID string) {
				defer wg.Done()
				defer func() { <-sem }()
				ctx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
				defer cancel()
				if d, err := activeGw.NetGameDetail(ctx, entityID); err == nil && len(d.BriefImageURLs) > 0 {
					a.skinImages.Store(entityID, d.BriefImageURLs[0])
				}
			}(id)
		}
		wg.Wait()
	}

	out := make([]SkinDTO, 0, len(items))
	for _, item := range items {
		img := ""
		if val, ok := a.skinImages.Load(item.EntityID); ok {
			img, _ = val.(string)
		}
		if img == "" {
			img = item.PreviewURL
		}
		if img == "" {
			img = item.TitleImageURL
		}
		if img == "" {
			img = item.ImageURL
		}
		if img == "" && len(item.BriefImageURLs) > 0 {
			img = item.BriefImageURLs[0]
		}
		out = append(out, SkinDTO{
			EntityID:   item.EntityID,
			Name:       item.Name,
			PreviewURL: img,
		})
	}
	return out, nil
}

func (a *App) ApplySkin(entityID string) error {
	if entityID == "" {
		return fmt.Errorf("皮肤 ID 不能为空")
	}
	return a.withGateway(func(gw *netease.Gateway) error {
		return gw.ApplySkin(a.ctx, entityID)
	})
}

// GetCurrentSkin 查询当前账号已装备的皮肤。
func (a *App) GetCurrentSkin() (SkinDTO, error) {
	var current SkinDTO
	err := a.withGateway(func(gw *netease.Gateway) error {
		hasSkin, skinID, err := gw.HasEquippedSkin(a.ctx)
		if err != nil {
			return err
		}
		if !hasSkin || skinID == "" {
			return nil
		}
		img := ""
		if val, ok := a.skinImages.Load(skinID); ok {
			img, _ = val.(string)
		}
		if img == "" {
			if batch, bErr := gw.BatchSkinImages(a.ctx, []string{skinID}); bErr == nil && batch[skinID] != "" {
				img = batch[skinID]
				a.skinImages.Store(skinID, img)
			}
		}
		current = SkinDTO{
			EntityID:   skinID,
			Name:       "当前皮肤",
			PreviewURL: img,
		}
		return nil
	})
	return current, err
}

// EnsureRandomSkinIfNone 检查用户是否已设置皮肤；若未设置则从可用免费皮肤中随机挑选并自动应用。
func (a *App) EnsureRandomSkinIfNone() (SkinDTO, error) {
	var targetSkin SkinDTO
	err := a.withGateway(func(gw *netease.Gateway) error {
		hasSkin, currentID, err := gw.HasEquippedSkin(a.ctx)
		if err == nil && hasSkin && currentID != "" {
			img := ""
			if val, ok := a.skinImages.Load(currentID); ok {
				img, _ = val.(string)
			}
			targetSkin = SkinDTO{
				EntityID:   currentID,
				Name:       "当前皮肤",
				PreviewURL: img,
			}
			return nil
		}

		// 用户未设置皮肤，随机挑选一款免费皮肤应用
		items, lErr := gw.ListFreeSkins(a.ctx, 0, 40)
		var pickedID string
		var pickedName string
		var pickedImg string
		if lErr == nil && len(items) > 0 {
			idx := int(time.Now().UnixNano() % int64(len(items)))
			pickedID = items[idx].EntityID
			pickedName = items[idx].Name
			pickedImg = items[idx].PreviewURL
		} else {
			fallbackIDs := []string{
				"4630108215716945668",
				"4635701020889602309",
				"4637651717887309990",
				"4637951556942918882",
			}
			idx := int(time.Now().UnixNano() % int64(len(fallbackIDs)))
			pickedID = fallbackIDs[idx]
			pickedName = "推荐免费皮肤"
		}

		if pickedID != "" {
			if aErr := gw.ApplySkin(a.ctx, pickedID); aErr != nil {
				return aErr
			}
			targetSkin = SkinDTO{
				EntityID:   pickedID,
				Name:       pickedName,
				PreviewURL: pickedImg,
			}
		}
		return nil
	})
	return targetSkin, err
}

// RandomizeSkin 随机挑选一款免费皮肤并应用（手动换肤触发）。
func (a *App) RandomizeSkin() (SkinDTO, error) {
	var targetSkin SkinDTO
	err := a.withGateway(func(gw *netease.Gateway) error {
		items, lErr := gw.ListFreeSkins(a.ctx, 0, 40)
		var pickedID string
		var pickedName string
		var pickedImg string
		if lErr == nil && len(items) > 0 {
			idx := int(time.Now().UnixNano() % int64(len(items)))
			pickedID = items[idx].EntityID
			pickedName = items[idx].Name
			pickedImg = items[idx].PreviewURL
		} else {
			fallbackIDs := []string{
				"4630108215716945668",
				"4635701020889602309",
				"4637651717887309990",
				"4637951556942918882",
			}
			idx := int(time.Now().UnixNano() % int64(len(fallbackIDs)))
			pickedID = fallbackIDs[idx]
			pickedName = "推荐免费皮肤"
		}

		if pickedID != "" {
			if aErr := gw.ApplySkin(a.ctx, pickedID); aErr != nil {
				return aErr
			}
			targetSkin = SkinDTO{
				EntityID:   pickedID,
				Name:       pickedName,
				PreviewURL: pickedImg,
			}
		}
		return nil
	})
	return targetSkin, err
}
