// 快速登录（会话复用）：用仍存活的会话 token 打 /authentication/update 心跳验证并换发，
// 成功即完成登录（不触碰任何登录链，零风控暴露）；任何失败返回 false，由 ReLogin 回落全链。
package service

import (
	"context"

	"nexus/internal/account"
)

// quickLoginBySession 用仍存活的会话 token 打 /authentication/update 心跳完成快速登录：
// 心跳同时验证会话并换发新 token（ReplaceTokenIfCurrent CAS 保存，不覆盖并发登录换发的 token）。
// 任何失败（token 缺失/解密失败/心跳失败）都返回 false，由 ReLogin 回落全链。
func (s *LoginService) quickLoginBySession(ctx context.Context, acct account.Account) (string, bool) {
	oldToken, err := s.accounts.Token(acct.ID)
	if err != nil || oldToken == "" {
		return "", false
	}
	newToken, err := s.refreshToken(ctx, acct.UserID, oldToken)
	if err != nil {
		if s.log != nil {
			s.log.Info("service", "会话心跳失败，回落全链重登", "accountRef", acct.Label, "err", err.Error())
		}
		return "", false
	}
	if newToken == "" {
		if s.log != nil {
			s.log.Info("service", "会话心跳响应缺少 token，回落全链重登", "accountRef", acct.Label)
		}
		return "", false
	}
	updated, err := s.accounts.ReplaceTokenIfCurrent(acct.ID, oldToken, newToken)
	if err != nil || !updated {
		// CAS 失败：并发登录已写入更新的 token，不再用旧换发结果覆盖。
		if s.log != nil {
			s.log.Info("service", "快速登录 token 未落库（并发登录已更新），沿用现存 token", "accountRef", acct.Label)
		}
		if cur, cerr := s.accounts.Token(acct.ID); cerr == nil && cur != "" {
			return cur, true
		}
		return "", false
	}
	return newToken, true
}
