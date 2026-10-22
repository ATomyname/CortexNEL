// 登录链收尾（facts §6.1/§6.1.2）：SAuth 会话校验 → login-otp → authentication-otp。
// 形状逐字段核对 Nirvana NPFLauncher.LoginWithCookieAsync / EntityLoginOtp /
// EntityAuthenticationData / EntityAuthenticationOtp / HttpUtil（规范 §10.11）。
package netease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nexus/internal/httpx"
)

// Session 是一次完整登录的结果（entity_id 即 userId）。
type Session struct {
	EntityID int64  `json:"entity_id"`
	Token    string `json:"token"`
	SDKUID   string `json:"sdkuid"`
	Channel  string `json:"channel"`
}

// SessionSource 提供 sauth cookie（UI 手动注入 / 4399 密码链产出，facts §6.1）。
type SessionSource interface {
	SAuthCookie(ctx context.Context) (string, error)
}

// cookieRequest 兼容解包结构体（对齐 EntityX19CookieRequest）。
type cookieRequest struct {
	SauthJSON string `json:"sauth_json"`
}

// cookieMeta 是 sauth JSON 核心元数据（对齐 EntityX19Cookie）。
type cookieMeta struct {
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

// unwrapCookie 规范化 Cookie 字符串，兼容裸 JSON 与 {"sauth_json":"..."} 外层包装。
func unwrapCookie(raw string) (string, cookieMeta) {
	raw = strings.TrimSpace(raw)
	sauth := raw

	var req cookieRequest
	if err := json.Unmarshal([]byte(raw), &req); err == nil && req.SauthJSON != "" {
		sauth = strings.TrimSpace(req.SauthJSON)
	}

	var meta cookieMeta
	_ = json.Unmarshal([]byte(sauth), &meta)
	if meta.LoginChannel == "" {
		meta.LoginChannel = "netease"
	}
	if meta.AppChannel == "" {
		meta.AppChannel = meta.LoginChannel
	}
	return sauth, meta
}

// CompleteLogin 完成 cookie → x19 token 的收尾链（facts §6.1.2）：
// 1) 仅非 netease 渠道服校验 mgb uni_sauth（官方 netease 跳过，对齐 OpenNEL/Fantnel/Codexus）
// 2) Core /login-otp：请求 {"sauth_json": sauthCookie}，信封取 entity.otp_token/aid
// 3) Core /authentication-otp：EntityAuthenticationData 经 HTTP 加密传输，解密信封取实体令牌
// 4) Core /login-start：建立互联会话激活令牌。
func CompleteLogin(ctx context.Context, source SessionSource) (Session, error) {
	rawCookie, err := source.SAuthCookie(ctx)
	if err != nil {
		return Session{}, err
	}
	sauthCookie, meta := unwrapCookie(rawCookie)
	if sauthCookie == "" {
		return Session{}, fmt.Errorf("netease: 无效的 Cookie 内容")
	}
	client := httpx.NewClient(20 * time.Second)

	// 1. uni_sauth 校验（对齐 OpenNEL/Fantnel/Codexus：仅对非 netease 渠道服进行校验；官方 netease 必须跳过）。
	if meta.LoginChannel != "netease" {
		if err := postJSON(ctx, client, MgbBaseURL+"/x19/sdk/uni_sauth", sauthCookie, nil); err != nil {
			return Session{}, fmt.Errorf("netease: uni_sauth: %w", err)
		}
	}

	// 2. login-otp（X19Extensions.Core token=false，无签名头，facts §6.1.2）。
	otpReq, err := json.Marshal(map[string]string{"sauth_json": sauthCookie})
	if err != nil {
		return Session{}, err
	}
	var otpEnv struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Entity  struct {
			OtpToken string `json:"otp_token"`
			Aid      int    `json:"aid"`
			Code     int    `json:"code"`
			SubCode  int    `json:"subcode"`
			Status   string `json:"status"`
		} `json:"entity"`
	}
	if err := postJSON(ctx, client, InterconnBaseURL+"/login-otp", string(otpReq), &otpEnv); err != nil {
		return Session{}, fmt.Errorf("netease: login-otp: %w", err)
	}
	if otpEnv.Code != 0 {
		// code=32 的「服务器维护中」是马甲：真实死因在 entity（实测 sauth 失效时 entity 为
		// code=401/subcode=0/status="auth fail"，而网易并不维护）。透出 entity 并给下一步，
		// 否则用户会照着「稍候再试」干等（facts §6.1.2；对照实验见 cmd/oauthprobe）。
		if otpEnv.Entity.Code != 0 || otpEnv.Entity.Status != "" {
			return Session{}, fmt.Errorf("netease: login-otp code=%d: %s（entity: code=%d subcode=%d status=%q）—— 该 sauth 未通过校验，请从小号机重新导出 sauth 或用账号密码重新登录",
				otpEnv.Code, otpEnv.Message, otpEnv.Entity.Code, otpEnv.Entity.SubCode, otpEnv.Entity.Status)
		}
		return Session{}, fmt.Errorf("netease: login-otp code=%d: %s", otpEnv.Code, otpEnv.Message)
	}
	if otpEnv.Entity.OtpToken == "" {
		return Session{}, fmt.Errorf("netease: login-otp 响应缺少 otp_token")
	}

	// 3. authentication-otp（请求/响应均 HTTP 加密，facts §6.1.2）。
	data, err := json.Marshal(buildAuthData(sauthCookie, meta, otpEnv.Entity.OtpToken, otpEnv.Entity.Aid))
	if err != nil {
		return Session{}, err
	}
	enc, err := httpEncrypt(data)
	if err != nil {
		return Session{}, fmt.Errorf("netease: authentication-otp 加密: %w", err)
	}
	raw, err := postRaw(ctx, client, InterconnBaseURL+"/authentication-otp", enc)
	if err != nil {
		return Session{}, fmt.Errorf("netease: authentication-otp: %w", err)
	}
	dec, err := httpDecrypt(raw)
	if err != nil {
		return Session{}, fmt.Errorf("netease: authentication-otp 解密: %w", err)
	}
	var authEnv struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Entity  struct {
			EntityID string `json:"entity_id"`
			Token    string `json:"token"`
			SDKUID   string `json:"sdkuid"`
		} `json:"entity"`
	}
	if err := json.Unmarshal(dec, &authEnv); err != nil {
		return Session{}, fmt.Errorf("netease: authentication-otp 响应解析: %w（%s）", err, truncate(dec, 200))
	}
	if authEnv.Code != 0 {
		return Session{}, fmt.Errorf("netease: authentication-otp code=%d: %s", authEnv.Code, authEnv.Message)
	}
	entityID, err := strconv.ParseInt(authEnv.Entity.EntityID, 10, 64)
	if err != nil || entityID == 0 || authEnv.Entity.Token == "" {
		return Session{}, fmt.Errorf("netease: authentication-otp 响应缺少 entity_id/token")
	}

	// 4. login-start 激活互联会话（对齐 Codexus / OpenNEL / Fantnel）。
	gw := NewGateway(Credentials{UserID: entityID, Token: authEnv.Entity.Token})
	_ = gw.InterconnLoginStart(ctx)

	return Session{
		EntityID: entityID,
		Token:    authEnv.Entity.Token,
		SDKUID:   authEnv.Entity.SDKUID,
		Channel:  meta.LoginChannel,
	}, nil
}

// authDetail 是 authentication-otp 的 sa_data 设备详情（facts §6.1.2，
// 默认值核对 EntityAuthenticationDetail 属性初始化器）。
type authDetail struct {
	OSName       string `json:"os_name"`
	OSVer        string `json:"os_ver"`
	MacAddr      string `json:"mac_addr"`
	Udid         string `json:"udid"`
	AppVer       string `json:"app_ver"`
	SdkVer       string `json:"sdk_ver"`
	Network      string `json:"network"`
	Disk         string `json:"disk"`
	Is64Bit      string `json:"is64bit"`
	VideoCard1   string `json:"video_card1"`
	VideoCard2   string `json:"video_card2"`
	VideoCard3   string `json:"video_card3"`
	VideoCard4   string `json:"video_card4"`
	LauncherType string `json:"launcher_type"`
	PayChannel   string `json:"pay_channel"`
	DotnetVer    string `json:"dotnet_ver"`
	CPUType      string `json:"cpu_type"`
	RamSize      string `json:"ram_size"`
	DeviceWidth  string `json:"device_width"`
	DeviceHeight string `json:"device_height"`
	OSDetail     string `json:"os_detail"`
}

// authData 是 /authentication-otp 请求体（facts §6.1.2，核对 EntityAuthenticationData）。
type authData struct {
	SaData     string      `json:"sa_data"`
	SauthJSON  string      `json:"sauth_json"`
	Version    authVersion `json:"version"`
	SdkUID     string      `json:"sdkuid"`
	Aid        string      `json:"aid"`
	OtpToken   string      `json:"otp_token"`
	OtpPwd     string      `json:"otp_pwd"`
	LockTime   int         `json:"lock_time"`
	Env        string      `json:"env"`
	IsRegister bool        `json:"is_register"`
	EntityID   string      `json:"entity_id"`
}

// authVersion 是请求体 version 块（facts §6.1.2）。
type authVersion struct {
	Version     string `json:"version"`
	LauncherMd5 string `json:"launcher_md5"`
	UpdaterMd5  string `json:"updater_md5"`
}

// buildAuthData 组装 authentication-otp 请求体（pay_channel 取 sauth 的 app_channel）。
func buildAuthData(cookie string, meta cookieMeta, otpToken string, aid int) authData {
	suffix, err := randomHexUpper(2)
	if err != nil {
		suffix = "0000"
	}
	mac, err := randomMAC()
	if err != nil {
		mac = "00:00:00:00:00:00"
	}
	payChannel := meta.AppChannel
	if payChannel == "" {
		payChannel = "netease"
	}
	detail := authDetail{
		OSName:       "windows",
		OSVer:        "Microsoft Windows 11 专业版",
		MacAddr:      mac,
		Udid:         "0000000000000000" + suffix,
		AppVer:       LauncherVersion,
		Disk:         suffix,
		Is64Bit:      "1",
		VideoCard1:   "Microsoft Hyper-V 视频",
		VideoCard2:   "Microsoft Remote Display Adapter",
		LauncherType: "PC_java",
		PayChannel:   payChannel,
		DotnetVer:    "4.8.0",
		CPUType:      "Intel(R) Core(TM) i9-14900KF",
		RamSize:      "8589934592",
		DeviceWidth:  "1920",
		DeviceHeight: "1080",
		OSDetail:     "10.0.26100",
	}
	saData, err := json.Marshal(detail)
	if err != nil {
		saData = []byte("{}")
	}
	return authData{
		SaData:    string(saData),
		SauthJSON: cookie,
		Version:   authVersion{Version: LauncherVersion},
		Aid:       strconv.Itoa(aid),
		OtpToken:  otpToken,
	}
}

// postRaw 发送原始字节请求并读回原始响应（HTTP 加密传输用）。
func postRaw(ctx context.Context, client *http.Client, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "WPFLauncher/"+LauncherVersion)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(data, 200))
	}
	return data, nil
}

// randomHexUpper 生成 n 字节的十六进制大写串（等价 StringGenerator.GenerateHexString(n)）。
func randomHexUpper(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(buf)), nil
}

// randomMAC 生成随机 MAC 串（等价 StringGenerator.GenerateRandomMacAddress）。
func randomMAC() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	parts := make([]string, 6)
	for i, b := range buf {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":"), nil
}

func postJSON(ctx context.Context, client *http.Client, url, rawBody string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(rawBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "WPFLauncher/"+LauncherVersion)
	return doJSON(ctx, client, req, result)
}

func doJSON(ctx context.Context, client *http.Client, req *http.Request, result any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(data, 200))
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("响应解析: %w（%s）", err, truncate(data, 200))
		}
	}
	return nil
}
