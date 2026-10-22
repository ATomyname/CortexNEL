// mPay 设备注册与邮箱密码登录（facts §6.3，逐字段核对归档 Codexus UniSdkMPay.cs）：
// service.mkey.163.com 域，form-urlencoded；产出 user{id,token} 供 x19 sauth 构造。
package netease

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nexus/internal/crypto"
	"nexus/internal/httpx"
	"nexus/internal/store"
)

// mPay 常量（facts §6.3；来源归档 Codexus Projects.cs / UniSdkMPay.BuildBaseParams）。
const (
	MPayBaseURL    = "https://service.mkey.163.com"
	MPayProjectX19 = "aecfrxodyqaaaajp-g-x19"
	MPayAppKey     = "EEkEEXLymcNjM42yLY3Bn6AO15aGy4yq"
	MPayCV         = "c4.2.0"
	MPayUpdaterCV  = "c1.0.0"
	MPayOSVersion  = "10.0.22621"
	MPaySDKVersion = "4.2.0"
)

// MPayDevice 是一次设备注册的凭据（id + 参数加密密钥，facts §6.3）。
type MPayDevice struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// MPayUser 是邮箱登录成功后的用户主体（facts §6.3，取 id/token 两键）。
type MPayUser struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type mpayTicket struct {
	Ticket string `json:"ticket"`
}

// mpayClient 可注入测试替身的 mPay 客户端（baseURL 覆盖用于 httptest）。
type mpayClient struct {
	baseURL string
	// mu 保护进程内设备档案：unique_id 与 device{id,key} 跨登录复用，
	// 避免每次登录都注册新设备（参考实现落盘缓存；本实现进程内缓存，facts §6.3 注）。
	// 注册失败不落档，下次登录重试注册。
	mu     sync.Mutex
	unique string
	device MPayDevice
	// cacheEnabled 仅对真实客户端开启；测试替身保持完全内存态。
	cacheEnabled bool
}

// defaultMPayClient 跨账号登录复用同一 mPay 设备。此前 X19EmailLogin 每次
// new 一个 mpayClient，导致每次点击登录都重新注册设备，极易触发 mPay 风控。
var defaultMPayClient = &mpayClient{baseURL: MPayBaseURL, cacheEnabled: true}

type persistedMPayDevice struct {
	UniqueID     string `json:"uniqueId"`
	DeviceID     string `json:"deviceId"`
	EncryptedKey string `json:"encryptedKey"`
}

// newX19EmailLogin 供 mpay_test 注入 httptest 地址。
func newX19EmailLogin(baseURL string) *mpayClient {
	return &mpayClient{baseURL: baseURL}
}

// X19EmailLogin 执行 facts §6.3 全链：设备（缓存）→ 邮箱登录 → 返回 sauth cookie
// JSON（netease 渠道，喂给 CompleteLogin 收尾链）。
func X19EmailLogin(ctx context.Context, email, password string) (string, error) {
	return defaultMPayClient.EmailLogin(ctx, email, password)
}

// X19RequestSMS 发送手机号登录短信验证码。
func X19RequestSMS(ctx context.Context, phone string) error {
	return defaultMPayClient.RequestSMS(ctx, phone)
}

// X19PhoneSMSLogin 使用手机号和短信验证码登录。
func X19PhoneSMSLogin(ctx context.Context, phone, code string) (string, error) {
	return defaultMPayClient.PhoneSMSLogin(ctx, phone, code)
}

// X19PhonePasswordLogin 使用手机号和密码登录。
func X19PhonePasswordLogin(ctx context.Context, phone, password string) (string, error) {
	return defaultMPayClient.PhonePasswordLogin(ctx, phone, password)
}

// EmailLogin 同 X19EmailLogin，保留在客户端上以便测试注入。
func (m *mpayClient) EmailLogin(ctx context.Context, email, password string) (string, error) {
	if strings.TrimSpace(email) == "" || password == "" {
		return "", fmt.Errorf("netease: 邮箱或密码不能为空")
	}
	device, err := m.ensureDevice(ctx)
	if err != nil {
		return "", err
	}
	user, err := m.loginEmail(ctx, device, email, password)
	if err != nil {
		return "", err
	}
	return buildX19SAuthCookie(user, device)
}

// RequestSMS 请求手机号登录短信验证码。
func (m *mpayClient) RequestSMS(ctx context.Context, phone string) error {
	if strings.TrimSpace(phone) == "" {
		return fmt.Errorf("netease: 手机号不能为空")
	}
	device, err := m.ensureDevice(ctx)
	if err != nil {
		return err
	}
	form := m.baseParams()
	form.Set("device_id", device.ID)
	form.Set("mobile", phone)
	return m.postForm(ctx, "/mpay/api/users/login/mobile/get_sms", form, &struct{}{})
}

// PhoneSMSLogin 使用手机号和短信验证码登录。
func (m *mpayClient) PhoneSMSLogin(ctx context.Context, phone, code string) (string, error) {
	if strings.TrimSpace(phone) == "" || strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("netease: 手机号和短信验证码不能为空")
	}
	device, err := m.ensureDevice(ctx)
	if err != nil {
		return "", err
	}
	form := m.baseParams()
	form.Set("device_id", device.ID)
	form.Set("mobile", phone)
	form.Set("smscode", code)
	form.Set("up_content", "")
	var ticket mpayTicket
	if err := m.postForm(ctx, "/mpay/api/users/login/mobile/verify_sms", form, &ticket); err != nil {
		return "", wrapMPayVerifyError(err)
	}
	user, err := m.finishPhoneLogin(ctx, device, phone, ticket)
	if err != nil {
		return "", err
	}
	return buildX19SAuthCookie(user, device)
}

// PhonePasswordLogin 使用手机号和密码登录。
func (m *mpayClient) PhonePasswordLogin(ctx context.Context, phone, password string) (string, error) {
	if strings.TrimSpace(phone) == "" || password == "" {
		return "", fmt.Errorf("netease: 手机号和密码不能为空")
	}
	device, err := m.ensureDevice(ctx)
	if err != nil {
		return "", err
	}
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	form := m.baseParams()
	form.Set("device_id", device.ID)
	form.Set("login_for", "1")
	form.Set("mcount_transaction_id", "3")
	form.Set("mobile", phone)
	form.Set("opt_fields", mpayScope)
	form.Set("password", md5Hex(password))
	form.Set("transid", deviceUniqueID(m)+"_"+now+"_100022997")
	form.Set("uni_transaction_id", deviceUniqueID(m)+"_"+now+"_100023001")
	form.Set("urs_udid", deviceUniqueID(m))
	var ticket mpayTicket
	path := "/mpay/api/users/login/mobile/verify_pwd?un=" + base64.StdEncoding.EncodeToString([]byte(phone))
	if err := m.postForm(ctx, path, form, &ticket); err != nil {
		return "", wrapMPayVerifyError(err)
	}
	user, err := m.finishPhoneLogin(ctx, device, phone, ticket)
	if err != nil {
		return "", err
	}
	return buildX19SAuthCookie(user, device)
}

const mpayScope = "nickname,avatar,realname_status,mobile_bind_status,mask_related_mobile,related_login_status"

func deviceUniqueID(m *mpayClient) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unique
}

func (m *mpayClient) finishPhoneLogin(ctx context.Context, device MPayDevice, phone string, ticket mpayTicket) (MPayUser, error) {
	if ticket.Ticket == "" {
		return MPayUser{}, fmt.Errorf("netease: mPay 响应缺少手机号登录 ticket")
	}
	form := m.baseParams()
	form.Set("device_id", device.ID)
	form.Set("opt_fields", mpayScope)
	form.Set("ticket", ticket.Ticket)
	path := "/mpay/api/users/login/mobile/finish?un=" + base64.StdEncoding.EncodeToString([]byte(phone))
	var env struct {
		User MPayUser `json:"user"`
	}
	if err := m.postForm(ctx, path, form, &env); err != nil {
		return MPayUser{}, wrapMPayVerifyError(err)
	}
	if env.User.ID == "" || env.User.Token == "" {
		return MPayUser{}, fmt.Errorf("netease: mPay 响应缺少 user.id/token")
	}
	return env.User, nil
}

func wrapMPayVerifyError(err error) error {
	if verify, parseErr := parseVerifyChallenge(err.Error()); parseErr == nil {
		return fmt.Errorf("%w：%s（官方验证入口: %s）", ErrMPayVerifyRequired, verify.Reason, verify.VerifyURL)
	}
	return err
}

// ensureDevice 进程内缓存设备；无缓存时注册并缓存（facts §6.3）。
func (m *mpayClient) ensureDevice(ctx context.Context) (MPayDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.device.ID != "" && m.device.Key != "" {
		return m.device, nil
	}
	if m.loadCachedDevice() {
		return m.device, nil
	}
	m.unique = randomHex32()
	device, err := m.registerDevice(ctx, m.unique)
	if err != nil {
		return MPayDevice{}, err
	}
	m.device = device
	m.saveCachedDevice()
	return device, nil
}

func (m *mpayClient) deviceCachePath() (string, error) {
	if !m.cacheEnabled {
		return "", errors.New("mPay device cache disabled")
	}
	root, err := store.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "mpay-device.json"), nil
}

func (m *mpayClient) loadCachedDevice() bool {
	path, err := m.deviceCachePath()
	if err != nil {
		return false
	}
	cached, _, err := store.Load[persistedMPayDevice](path)
	if err != nil || cached.UniqueID == "" || cached.DeviceID == "" || cached.EncryptedKey == "" {
		return false
	}
	key, err := store.UnprotectString(cached.EncryptedKey)
	if err != nil || key == "" {
		return false
	}
	m.unique = cached.UniqueID
	m.device = MPayDevice{ID: cached.DeviceID, Key: key}
	return true
}

func (m *mpayClient) saveCachedDevice() {
	path, err := m.deviceCachePath()
	if err != nil || m.unique == "" || m.device.ID == "" || m.device.Key == "" {
		return
	}
	key, err := store.ProtectString(m.device.Key)
	if err != nil {
		return
	}
	_ = store.Save(path, persistedMPayDevice{
		UniqueID:     m.unique,
		DeviceID:     m.device.ID,
		EncryptedKey: key,
	})
}

// registerDevice POST /mpay/games/{project}/devices（facts §6.3 表单逐字段核对）。
func (m *mpayClient) registerDevice(ctx context.Context, uniqueID string) (MPayDevice, error) {
	form := m.baseParams()
	form.Set("unique_id", uniqueID)
	form.Set("brand", "Microsoft")
	form.Set("device_model", "pc_mode")
	form.Set("device_name", "PC-"+randomAlnum(12))
	form.Set("device_type", "Computer")
	form.Set("init_urs_device", "0")
	mac, err := randomMAC()
	if err != nil {
		return MPayDevice{}, err
	}
	form.Set("mac", mac)
	form.Set("resolution", "1920x1080")
	form.Set("system_name", "windows")
	form.Set("system_version", MPayOSVersion)

	var env struct {
		Device MPayDevice `json:"device"`
	}
	if err := m.postForm(ctx, "/mpay/games/"+MPayProjectX19+"/devices", form, &env); err != nil {
		return MPayDevice{}, err
	}
	return env.Device, nil
}

// loginEmail POST /mpay/games/{project}/devices/{deviceID}/users（facts §6.3）。
// params = AES-ECB-PKCS7(key=device.key, JSON{password:md5,unique_id,username}) hex；
// 非 200 时 code 1351 = 需风控验证（RunNEL 翻译语义）。
func (m *mpayClient) loginEmail(ctx context.Context, device MPayDevice, email, password string) (MPayUser, error) {
	key, err := hex.DecodeString(device.Key)
	if err != nil {
		return MPayUser{}, fmt.Errorf("netease: mPay 设备密钥非法: %w", err)
	}
	plain, err := json.Marshal(struct {
		Password string `json:"password"`
		UniqueID string `json:"unique_id"`
		Username string `json:"username"`
	}{Password: md5Hex(password), UniqueID: m.unique, Username: email})
	if err != nil {
		return MPayUser{}, err
	}
	enc, err := crypto.ECBEncryptPKCS7(key, plain)
	if err != nil {
		return MPayUser{}, fmt.Errorf("netease: mPay 参数加密: %w", err)
	}

	form := m.baseParams()
	form.Set("opt_fields", mpayScope)
	form.Set("params", hex.EncodeToString(enc))
	form.Set("un", base64.StdEncoding.EncodeToString([]byte(email)))

	path := "/mpay/games/" + MPayProjectX19 + "/devices/" + device.ID + "/users"
	var env struct {
		User MPayUser `json:"user"`
	}
	if err := m.postForm(ctx, path, form, &env); err != nil {
		// 1351 = 账号被风控标记（facts §6.3，RunNEL VerifyException 对应）：
		// 响应体 {"reason":"...","code":1351,"verify_url":"..."}，reason 为
		// JSON unicode 转义——解码后连同 verify_url 一并带出，用户可直接打开验证。
		if verify, parseErr := parseVerifyChallenge(err.Error()); parseErr == nil {
			return MPayUser{}, fmt.Errorf("%w：%s（官方验证入口: %s）",
				ErrMPayVerifyRequired, verify.Reason, verify.VerifyURL)
		}
		return MPayUser{}, err
	}
	if env.User.ID == "" || env.User.Token == "" {
		return MPayUser{}, fmt.Errorf("netease: mPay 响应缺少 user.id/token")
	}
	return env.User, nil
}

// baseParams 组装 mPay 公共表单参数（facts §6.3，字段序与归档实现一致）。
func (m *mpayClient) baseParams() url.Values {
	form := url.Values{}
	form.Set("app_channel", "netease")
	form.Set("app_mode", "2")
	form.Set("app_type", "games")
	form.Set("arch", "win_x64")
	form.Set("cv", MPayCV)
	form.Set("mcount_app_key", MPayAppKey)
	form.Set("mcount_transaction_id", "0")
	form.Set("process_id", strconv.Itoa(os.Getpid()))
	form.Set("sv", MPayOSVersion)
	form.Set("updater_cv", MPayUpdaterCV)
	form.Set("game_id", MPayProjectX19)
	form.Set("gv", LauncherVersion)
	return form
}

// postForm 发表单请求并解析 JSON 信封；非 200 时保留响应体摘要用于错误分类。
func (m *mpayClient) postForm(ctx context.Context, path string, form url.Values, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "WPFLauncher/"+LauncherVersion)
	client := httpx.NewClient(20 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("netease: mPay HTTP %d: %s", resp.StatusCode, truncate(data, 200))
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("netease: mPay 响应解析: %w（%s）", err, truncate(data, 200))
	}
	return nil
}

// x19SAuthJSON 是 x19 渠道 sauth 会话 JSON（facts §6.3，字段序对齐 X19SAuthJson）。
type x19SAuthJSON struct {
	GameID       string `json:"gameid"`
	LoginChannel string `json:"login_channel"`
	AppChannel   string `json:"app_channel"`
	Platform     string `json:"platform"`
	SdkUID       string `json:"sdkuid"`
	SessionID    string `json:"sessionid"`
	SdkVersion   string `json:"sdk_version"`
	Udid         string `json:"udid"`
	DeviceID     string `json:"deviceid"`
	AimInfo      string `json:"aim_info"`
}

// buildX19SAuthCookie 组装 x19 渠道 sauth JSON（喂给 CompleteLogin 收尾链）。
func buildX19SAuthCookie(user MPayUser, device MPayDevice) (string, error) {
	out, err := json.Marshal(x19SAuthJSON{
		GameID:       "x19",
		LoginChannel: "netease",
		AppChannel:   "netease",
		Platform:     "pc",
		SdkUID:       user.ID,
		SessionID:    user.Token,
		SdkVersion:   MPaySDKVersion,
		Udid:         randomHex32(),
		DeviceID:     device.ID,
		AimInfo:      `{"aim":"127.0.0.1","country":"CN","tz":"+0800","tzid":""}`,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// mpayVerifyChallenge 是 mPay 风控裁决体（facts §6.3：{"reason","code":1351,"verify_url"}）。
type mpayVerifyChallenge struct {
	Reason    string `json:"reason"`
	Code      int    `json:"code"`
	VerifyURL string `json:"verify_url"`
}

// ErrMPayVerifyRequired 表示 mPay 登录被风控拦截（1351），需官方端完成安全验证。
var ErrMPayVerifyRequired = errors.New("账号需网易安全验证")

// parseVerifyChallenge 从 postForm 的错误文本中提取 1351 风控裁决：
// 错误串形如 `netease: mPay HTTP 400: {json}`，从中截出 JSON 解码；
// 仅 code=1351 时返回结果。unicode 转义由 encoding/json 解码。
func parseVerifyChallenge(errText string) (mpayVerifyChallenge, error) {
	if !strings.Contains(errText, `"code":1351`) && !strings.Contains(errText, `"code": 1351`) {
		return mpayVerifyChallenge{}, errors.New("非 1351 裁决")
	}
	start := strings.Index(errText, "{")
	end := strings.LastIndex(errText, "}")
	if start < 0 || end <= start {
		return mpayVerifyChallenge{}, errors.New("裁决体缺失 JSON")
	}
	var challenge mpayVerifyChallenge
	if err := json.Unmarshal([]byte(errText[start:end+1]), &challenge); err != nil {
		return mpayVerifyChallenge{}, err
	}
	if challenge.Code != 1351 {
		return mpayVerifyChallenge{}, errors.New("code 非 1351")
	}
	return challenge, nil
}

// md5Hex 小写十六进制 MD5（facts §6.3：mPay password 字段）。
func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// randomHex32 生成 32 位大写十六进制（等价 Guid-N 大写，facts §6.3 udid）。
func randomHex32() string {
	s, err := randomHexUpper(16)
	if err != nil {
		return "00000000000000000000000000000000"
	}
	return s
}

// randomAlnum 生成 n 位小写字母数字随机串（等价 StringGenerator 12 位设备名后缀）。
func randomAlnum(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	buf, err := randomHexUpper(n)
	if err != nil {
		return strings.Repeat("a", n)
	}
	out := make([]byte, n)
	for i, b := range []byte(buf[:n]) {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out)
}
