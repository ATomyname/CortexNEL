package netease

// 4399com 密码链手动集成测试（需真实凭据/网络，默认跳过——不进 CI）。
//
// fetch  取一张验证码存盘（人工识图 / 调试）：
//   $env:TEST4399_MODE='fetch'; go test -run TestManual4399Login ./internal/netease/ -v
// login  用人工识读的验证码走完整链（密码段 + uni_sauth → otp 收尾）：
//   $env:TEST4399_MODE='login'; $env:TEST4399_ACCOUNT=<账号>; $env:TEST4399_PASSWORD=<密码>;
//   $env:TEST4399_CAPTCHA_ID=<fetch 输出>; $env:TEST4399_CAPTCHA=<识别结果>;
//   go test -run TestManual4399Login ./internal/netease/ -v
// auto   全自动单发（OCR 识别验证码；无置信度门控，诊断用）：
//   $env:TEST4399_MODE='auto'; $env:TEST4399_ACCOUNT=<账号>; $env:TEST4399_PASSWORD=<密码>;
//   go test -run TestManual4399Login ./internal/netease/ -v
//
// 约束：凭据只经环境变量注入；日志仅输出脱敏前缀（规范 S3）。
import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nexus/internal/captchaocr"
)

// testCookieSource 是测试内的 sauth 来源（等价 service.CookieSource，避免反向依赖）。
type testCookieSource struct{ cookie string }

func (s testCookieSource) SAuthCookie(context.Context) (string, error) {
	return s.cookie, nil
}

func TestManual4399Login(t *testing.T) {
	switch mode := os.Getenv("TEST4399_MODE"); mode {
	case "":
		t.Skip("手动集成测试：设置 TEST4399_MODE=fetch|login|auto 后运行")
	case "fetch":
		ctx := context.Background()
		id, err := NewCaptchaID()
		if err != nil {
			t.Fatalf("生成 captchaId: %v", err)
		}
		client, err := NewCom4399Client()
		if err != nil {
			t.Fatalf("构造客户端: %v", err)
		}
		img, err := client.CaptchaImage(ctx, id)
		if err != nil {
			t.Fatalf("拉取验证码: %v", err)
		}
		file := filepath.Join(os.TempDir(), "nexus-captcha-"+id[:12]+".png")
		if err := os.WriteFile(file, img, 0o600); err != nil {
			t.Fatalf("写验证码文件: %v", err)
		}
		t.Logf("CAPTCHA_ID=%s", id)
		t.Logf("CAPTCHA_FILE=%s", file)
	case "login":
		ctx := context.Background()
		accountName := os.Getenv("TEST4399_ACCOUNT")
		password := os.Getenv("TEST4399_PASSWORD")
		captchaID := os.Getenv("TEST4399_CAPTCHA_ID")
		code := os.Getenv("TEST4399_CAPTCHA")
		if accountName == "" || password == "" || captchaID == "" || code == "" {
			t.Fatalf("缺少 TEST4399_ACCOUNT/PASSWORD/CAPTCHA_ID/CAPTCHA 环境变量")
		}
		client, err := NewCom4399Client()
		if err != nil {
			t.Fatalf("构造客户端: %v", err)
		}
		sauth, err := client.Login(ctx, accountName, password, captchaID, code)
		if err != nil {
			t.Fatalf("密码段失败: %v", err)
		}
		t.Logf("SAUTH_OK len=%d prefix=%s...", len(sauth), maskedPrefix(sauth, 48))
		session, err := CompleteLogin(ctx, testCookieSource{cookie: sauth})
		if err != nil {
			t.Fatalf("收尾链失败（uni_sauth/otp）: %v", err)
		}
		t.Logf("LOGIN_OK entity_id=%d sdkuid_prefix=%s... token_prefix=%s...",
			session.EntityID, maskedPrefix(session.SDKUID, 16), maskedPrefix(session.Token, 12))
	case "auto":
		ctx := context.Background()
		accountName := os.Getenv("TEST4399_ACCOUNT")
		password := os.Getenv("TEST4399_PASSWORD")
		if accountName == "" || password == "" {
			t.Fatalf("缺少 TEST4399_ACCOUNT/PASSWORD 环境变量")
		}
		rec, err := captchaocr.New()
		if err != nil {
			t.Fatalf("识别器初始化: %v", err)
		}
		client, err := NewCom4399Client()
		if err != nil {
			t.Fatalf("构造客户端: %v", err)
		}
		id, err := NewCaptchaID()
		if err != nil {
			t.Fatalf("生成 captchaId: %v", err)
		}
		img, err := client.CaptchaImage(ctx, id)
		if err != nil {
			t.Fatalf("拉取验证码: %v", err)
		}
		code, err := rec.Recognize(img)
		if err != nil {
			t.Fatalf("验证码识别: %v", err)
		}
		t.Logf("OCR_CODE=%s", code)
		sauth, err := client.Login(ctx, accountName, password, id, code)
		if err != nil {
			t.Fatalf("密码段失败: %v", err)
		}
		t.Logf("SAUTH_OK len=%d prefix=%s...", len(sauth), maskedPrefix(sauth, 48))
		session, err := CompleteLogin(ctx, testCookieSource{cookie: sauth})
		if err != nil {
			t.Fatalf("收尾链失败（uni_sauth/otp）: %v", err)
		}
		t.Logf("LOGIN_OK entity_id=%d sdkuid_prefix=%s... token_prefix=%s...",
			session.EntityID, maskedPrefix(session.SDKUID, 16), maskedPrefix(session.Token, 12))
	default:
		t.Fatalf("未知 TEST4399_MODE=%s", mode)
	}
}

// maskedPrefix 截取脱敏前缀（规范 S3）。
func maskedPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
