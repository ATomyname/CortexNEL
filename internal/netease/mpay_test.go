package netease

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nexus/internal/crypto"
)

// newMPayTestServer 起一个模拟 mPay 域：设备注册返回固定 id/key，
// 邮箱登录校验参数解密形状后返回 user。返回客户端与请求记录通道。
// handler 在响应前完成 ParseForm：body 随响应关闭，表单缓存后测试侧再读才有效。
func newMPayTestServer(t *testing.T, userHandler http.HandlerFunc) (*mpayClient, <-chan *http.Request) {
	t.Helper()
	reqs := make(chan *http.Request, 8)
	mux := http.NewServeMux()
	mux.HandleFunc("/mpay/games/"+MPayProjectX19+"/devices", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		reqs <- r
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device": map[string]string{"id": "dev-1", "key": "0102030405060708090a0b0c0d0e0f10"},
		})
	})
	mux.HandleFunc("/mpay/games/"+MPayProjectX19+"/devices/dev-1/users", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		reqs <- r
		userHandler(w, r)
	})
	mux.HandleFunc("/mpay/api/users/login/mobile/get_sms", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		reqs <- r
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200})
	})
	mux.HandleFunc("/mpay/api/users/login/mobile/verify_sms", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		reqs <- r
		_ = json.NewEncoder(w).Encode(map[string]string{"ticket": "ticket-sms"})
	})
	mux.HandleFunc("/mpay/api/users/login/mobile/verify_pwd", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		reqs <- r
		_ = json.NewEncoder(w).Encode(map[string]string{"ticket": "ticket-pwd"})
	})
	mux.HandleFunc("/mpay/api/users/login/mobile/finish", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		reqs <- r
		_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": "42", "token": "tok-phone"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newX19EmailLogin(srv.URL), reqs
}

func TestMPayPhonePasswordLogin(t *testing.T) {
	client, reqs := newMPayTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": "unused", "token": "unused"}})
	})
	cookie, err := client.PhonePasswordLogin(context.Background(), "13800138000", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cookie, `"sdkuid":"42"`) || !strings.Contains(cookie, `"sessionid":"tok-phone"`) {
		t.Fatalf("phone password cookie=%s", cookie)
	}
	if r := <-reqs; !strings.HasSuffix(r.URL.Path, "/devices") {
		t.Fatalf("first request should register device: %s", r.URL.Path)
	}
	for i := 0; i < 2; i++ {
		r := <-reqs
		if !strings.Contains(r.URL.Path, "verify_pwd") && !strings.HasSuffix(r.URL.Path, "/finish") {
			t.Fatalf("unexpected phone password path %s", r.URL.Path)
		}
		if strings.Contains(r.URL.Path, "verify_pwd") {
			if r.Form.Get("password") != md5Hex("pw") || r.Form.Get("mobile") != "13800138000" {
				t.Fatalf("phone password form=%v", r.Form)
			}
		}
	}
}

func TestMPayPhoneSMSLogin(t *testing.T) {
	client, reqs := newMPayTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	if err := client.RequestSMS(context.Background(), "13800138000"); err != nil {
		t.Fatal(err)
	}
	cookie, err := client.PhoneSMSLogin(context.Background(), "13800138000", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cookie, `"sessionid":"tok-phone"`) {
		t.Fatalf("phone sms cookie=%s", cookie)
	}
	paths := []string{(<-reqs).URL.Path, (<-reqs).URL.Path, (<-reqs).URL.Path, (<-reqs).URL.Path}
	if !strings.HasSuffix(paths[0], "/devices") || !strings.HasSuffix(paths[1], "get_sms") || !strings.HasSuffix(paths[2], "verify_sms") || !strings.HasSuffix(paths[3], "finish") {
		t.Fatalf("phone sms paths=%v", paths)
	}
}

// TestX19EmailLoginProducesNeteaseSAuthCookie 全链形状：设备注册 → 邮箱登录 →
// sauth cookie 为 netease 渠道且 sdkuid/sessionid 取自 mPay user。
func TestX19EmailLoginProducesNeteaseSAuthCookie(t *testing.T) {
	client, _ := newMPayTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]string{"id": "42", "token": "tok-1"},
		})
	})
	cookie, err := client.EmailLogin(context.Background(), "user@example.com", "pw")
	if err != nil {
		t.Fatalf("EmailLogin: %v", err)
	}

	var meta cookieMeta
	if err := json.Unmarshal([]byte(cookie), &meta); err != nil {
		t.Fatalf("sauth 非 JSON: %v", err)
	}
	if meta.LoginChannel != "netease" || meta.AppChannel != "netease" || meta.Platform != "pc" {
		t.Fatalf("渠道字段不符：%+v", meta)
	}
	if meta.GameID != "x19" || meta.SdkUID != "42" || meta.SessionID != "tok-1" || meta.DeviceID != "dev-1" {
		t.Fatalf("sauth 核心字段不符：%+v", meta)
	}
	// 收尾链约定：netease 渠道跳过 uni_sauth（CompleteLogin 分支依据）。
	sauth, meta2 := unwrapCookie(cookie)
	if meta2.LoginChannel != "netease" || sauth != cookie {
		t.Fatalf("unwrapCookie 应原样透传裸 JSON")
	}
}

func TestMPayDeviceIsReusedAcrossLogins(t *testing.T) {
	client, reqs := newMPayTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": "42", "token": "tok-1"}})
	})
	for i := 0; i < 2; i++ {
		if _, err := client.EmailLogin(context.Background(), "user@example.com", "pw"); err != nil {
			t.Fatal(err)
		}
	}
	devices := 0
	users := 0
	for {
		select {
		case r := <-reqs:
			if strings.HasSuffix(r.URL.Path, "/devices") {
				devices++
			} else {
				users++
			}
		default:
			if devices != 1 || users != 2 {
				t.Fatalf("requests: devices=%d users=%d", devices, users)
			}
			return
		}
	}
}

// TestX19EmailLoginEncryptsParamsWithDeviceKey 邮箱登录 params 必须用设备 key
// 的 AES-ECB-PKCS7 加密，明文形状 password=md5 / unique_id / username 对齐 facts §6.3。
func TestX19EmailLoginEncryptsParamsWithDeviceKey(t *testing.T) {
	client, reqs := newMPayTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]string{"id": "42", "token": "tok-1"},
		})
	})
	if _, err := client.EmailLogin(context.Background(), "user@example.com", "pw"); err != nil {
		t.Fatal(err)
	}

	// 取邮箱登录请求（第二个）。
	<-reqs
	r := <-reqs
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	key, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f10")
	plain, err := crypto.ECBDecryptPKCS7(key, mustHex(t, r.Form.Get("params")))
	if err != nil {
		t.Fatalf("params 应可用设备 key 解密: %v", err)
	}
	var payload struct {
		Password string `json:"password"`
		UniqueID string `json:"unique_id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatalf("params 明文非 JSON: %v", err)
	}
	if payload.Username != "user@example.com" {
		t.Fatalf("username = %q", payload.Username)
	}
	if payload.UniqueID == "" {
		t.Fatal("unique_id 为空")
	}
	if got, want := unbase64(t, r.Form.Get("un")), "user@example.com"; got != want {
		t.Fatalf("un = %q, want %q", got, want)
	}
}

// TestX19EmailLoginRejects1351 1351 风控裁决上抛为可读错误（reason 解码 + verify_url 带出）。
func TestX19EmailLoginRejects1351(t *testing.T) {
	client, _ := newMPayTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":1351,"reason":"need verify","verify_url":"https://service.mkey.163.com/mpay/api/reverify/upload_x"}`))
	})
	_, err := client.EmailLogin(context.Background(), "user@example.com", "pw")
	if err == nil || !strings.Contains(err.Error(), "安全验证") || !strings.Contains(err.Error(), "need verify") {
		t.Fatalf("1351 应上抛可读风控错误，实得 %v", err)
	}
	if !strings.Contains(err.Error(), "reverify/upload_x") {
		t.Fatalf("verify_url 应随错误带出: %v", err)
	}
}

// TestX19EmailLoginRequiresParams 空入参在登录前拒绝，不打任何网络请求。
func TestX19EmailLoginRequiresParams(t *testing.T) {
	client, reqs := newMPayTestServer(t, func(http.ResponseWriter, *http.Request) {})
	if _, err := client.EmailLogin(context.Background(), "", "pw"); err == nil {
		t.Fatal("空邮箱应报错")
	}
	select {
	case r := <-reqs:
		t.Fatalf("空入参不得发起请求：%s", r.URL.Path)
	default:
	}
}

// TestParseVerifyChallenge1351 真机实测的 1351 裁决体（unicode 转义 reason + verify_url）：
// 从 postForm 错误文本中解码出 reason 与官方验证入口。
func TestParseVerifyChallenge1351(t *testing.T) {
	errText := `netease: mPay HTTP 400: {"reason":"\u8d26\u53f7\u5b58\u5728\u5b89\u5168\u98ce\u9669\uff0c\u8bf7\u8fdb\u884c\u5b89\u5168\u9a8c\u8bc1\u3002", "code": 1351, "verify_url": "https://service.mkey.163.com/mpay/api/reverify/upload_xxx"}`
	challenge, err := parseVerifyChallenge(errText)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if challenge.Code != 1351 {
		t.Fatalf("code = %d", challenge.Code)
	}
	if challenge.Reason != "账号存在安全风险，请进行安全验证。" {
		t.Fatalf("reason 解码不符: %q", challenge.Reason)
	}
	if !strings.HasPrefix(challenge.VerifyURL, "https://service.mkey.163.com/mpay/api/reverify/") {
		t.Fatalf("verify_url = %q", challenge.VerifyURL)
	}
	// 非 1351 裁决不解析。
	if _, err := parseVerifyChallenge(`netease: mPay HTTP 400: {"reason":"x","code":500}`); err == nil {
		t.Fatal("非 1351 应报错")
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("params 非 hex: %v", err)
	}
	return b
}

func unbase64(t *testing.T, s string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("un 非 base64: %v", err)
	}
	return string(b)
}
