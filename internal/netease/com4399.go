// 4399com OAuth 密码登录链：OAuth 设备注册 -> authorize 隐藏表单 -> 服务器验证码 -> 登录重定向。
package netease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"nexus/internal/httpx"
)

const (
	Com4399OAuthCallbackURL = "https://m.4399api.com/openapi/oauth-callback.html?gamekey=44770&game_key=115716"
	Com4399LoginURL         = "https://ptlogin.4399.com/oauth2/loginAndAuthorize.do?channel=&sdk=op"
	Com4399CaptchaBaseURL   = "https://ptlogin.4399.com/ptlogin/captcha.do?captchaId="
	com4399OAuthURL         = "https://m.4399api.com/openapiv2/oauth.html"
	com4399BizID            = "2100001792"
	com4399UserAgent        = "WPFLauncher/0.0.0.0"
)

var inputTagRE = regexp.MustCompile(`(?is)<input\b[^>]*>`)
var inputAttrRE = regexp.MustCompile(`(?is)\b([a-zA-Z_:][a-zA-Z0-9_.:-]*)\s*=\s*["']([^"']*)["']`)
var loginTipRE = regexp.MustCompile(`(?is)<p\s+class=["'][^"']*(?:warning_tips|ipt_tips_err)[^"']*["'][^>]*id=["'](?:login_err_msg|captcha1_err_msg)["'][^>]*>\s*(.*?)\s*</p>`)
var captchaIDRE = regexp.MustCompile(`(?i)(?:captcha_id|captchaId|captchaid)\s*[:=]\s*["']?([a-z0-9_-]+)`)

// NewCaptchaID 保留给旧的手动诊断入口；正式登录使用 CaptchaChallenge 返回的服务器 ID。
func NewCaptchaID() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

type Com4399Client struct {
	HTTP      *http.Client
	authorize string
	initial   url.Values
	form      url.Values
	captchaID string
}

func NewCom4399Client() (*Com4399Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := httpx.NewClient(30 * time.Second)
	c.Jar = jar
	// OAuth 成功以 302 携带会话结果；由调用方读取 Location，避免 Go 自动跟随丢失状态。
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Com4399Client{HTTP: c}, nil
}

// CaptchaChallenge 注册 OAuth 设备并从 authorize 页面取得真实验证码 ID 与图片。
func (c *Com4399Client) CaptchaChallenge(ctx context.Context) (string, []byte, error) {
	if err := c.prepareOAuth(ctx); err != nil {
		return "", nil, err
	}
	form := cloneValues(c.initial)
	form.Set("auth_action", "ORILOGIN")
	status, _, body, err := c.request(ctx, http.MethodPost, c.authorizeEndpoint("/oauth2/authorize.do?channel=&sdk=op"), []byte(form.Encode()))
	if err != nil {
		return "", nil, fmt.Errorf("4399: authorize 初始化失败: %w", err)
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("4399: authorize 初始化 HTTP %d: %s", status, truncate(body, 200))
	}
	c.form = parseHiddenForms(string(body))
	c.captchaID = extractCaptchaID(string(body), c.form)
	if c.captchaID == "" {
		// 有些 authorize 页面没有回传 captcha_id；ptlogin 接口仍接受客户端生成的
		// 96 位十六进制 ID（旧版 4399mixed SDK 使用的兼容路径）。
		c.captchaID, err = NewCaptchaID()
		if err != nil {
			return "", nil, fmt.Errorf("4399: 生成验证码 ID 失败: %w", err)
		}
	}
	img, err := c.CaptchaImage(ctx, c.captchaID)
	if err != nil {
		return "", nil, err
	}
	return c.captchaID, img, nil
}

func extractCaptchaID(page string, form url.Values) string {
	for _, key := range []string{"captcha_id", "captchaId", "captchaid"} {
		if id := strings.TrimSpace(form.Get(key)); id != "" {
			return id
		}
	}
	for _, tag := range inputTagRE.FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, match := range inputAttrRE.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(match[1])] = html.UnescapeString(match[2])
		}
		name := strings.ToLower(attrs["name"])
		if name == "captcha_id" || name == "captchaid" {
			return attrs["value"]
		}
	}
	match := captchaIDRE.FindStringSubmatch(page)
	if len(match) == 2 {
		return html.UnescapeString(match[1])
	}
	return ""
}

func (c *Com4399Client) CaptchaImage(ctx context.Context, captchaID string) ([]byte, error) {
	status, _, body, err := c.request(ctx, http.MethodGet, Com4399CaptchaBaseURL+url.QueryEscape(captchaID), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("netease: 验证码图片 HTTP %d", status)
	}
	return body, nil
}

// PrimePE 注册 PE 线设备身份；与 OAuth 密码链共用 Cookie 容器。
func (c *Com4399Client) PrimePE(ctx context.Context) error {
	deviceID := newGUID32()
	device := map[string]any{
		"DEVICE_IDENTIFIER": deviceID, "SCREEN_RESOLUTION": "3200*1384", "DEVICE_MODEL": "Oppo A5 2022",
		"DEVICE_MODEL_VERSION": "14", "SYSTEM_VERSION": "14", "PLATFORM_TYPE": "Android", "SDK_VERSION": "3.12.2.503",
		"GAME_KEY": "115716", "GAME_VERSION": "3.1.5.260925", "BID": "com.netease.mc.m4399", "RUNTIME": "Origin",
		"CANAL_IDENTIFIER": "", "UDID": deviceID, "DEBUG": "false", "NETWORK_TYPE": "WIFI", "GAME_BOX_VERSION": "",
		"VIP_INFO": "", "TEAM": -53687088, "DEVICE_IDENTIFIER_SM": deviceID, "UID": "",
	}
	deviceJSON, err := json.Marshal(device)
	if err != nil {
		return err
	}
	form := url.Values{"usernames": {""}, "top_bar": {"1"}, "state": {""}, "device": {string(deviceJSON)}}
	status, _, body, err := c.request(ctx, http.MethodPost, com4399OAuthURL, []byte(form.Encode()))
	if err != nil {
		return fmt.Errorf("netease: PE 设备注册失败: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("netease: PE 设备注册 HTTP %d: %s", status, truncate(body, 200))
	}
	return nil
}

// Login 使用 CaptchaChallenge 保存的 authorize 表单提交凭据并解析 302 后的 sauth。
func (c *Com4399Client) Login(ctx context.Context, username, password, captchaID, captchaCode string) (string, error) {
	if c.form == nil || c.authorize == "" {
		if _, _, err := c.CaptchaChallenge(ctx); err != nil {
			return "", err
		}
	}
	if captchaID == "" {
		captchaID = c.captchaID
	}
	form := cloneValues(c.form)
	form.Set("isInputRealname", "false")
	form.Set("isVaildRealname", "false")
	form.Set("sec", "0")
	form.Set("response_type", "TOKEN")
	form.Set("scope", "basic")
	form.Set("bizId", com4399BizID)
	form.Set("auth_action", "ORILOGIN")
	form.Set("redirect_uri", Com4399OAuthCallbackURL)
	form.Set("username", username)
	form.Set("password", password)
	form.Set("captcha_id", captchaID)
	form.Set("captcha", captchaCode)
	status, location, body, err := c.request(ctx, http.MethodPost, c.authorizeEndpoint("/oauth2/loginAndAuthorize.do?channel=&sdk=op"), []byte(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("4399: 登录请求失败: %w", err)
	}
	if tip := extractLoginErrTip(string(body)); tip != "" {
		return "", fmt.Errorf("4399: %s", tip)
	}
	if status == http.StatusAccepted {
		return "", fmt.Errorf("4399: 登录被风控拦截: %s", truncate(body, 300))
	}
	if status >= 300 && status < 400 {
		if location == "" {
			return "", fmt.Errorf("4399: 登录重定向缺少 Location")
		}
		status, _, body, err = c.follow(ctx, location)
		if err != nil {
			return "", fmt.Errorf("4399: 登录重定向失败: %w", err)
		}
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("4399: 登录响应 HTTP %d: %s", status, truncate(body, 300))
	}
	return c.buildSAuth(body)
}

func (c *Com4399Client) prepareOAuth(ctx context.Context) error {
	if c.authorize != "" && c.initial != nil {
		return nil
	}
	deviceID := newGUID32()
	device := map[string]any{"DEVICE_IDENTIFIER": deviceID, "UDID": deviceID, "GAME_KEY": "115716", "BID": "com.netease.mc.m4399", "PLATFORM_TYPE": "Android", "DEVICE_MODEL": "Oppo A5 2022", "SYSTEM_VERSION": "14", "SDK_VERSION": "3.12.2.503", "RUNTIME": "Origin"}
	raw, err := json.Marshal(device)
	if err != nil {
		return err
	}
	form := url.Values{"usernames": {""}, "top_bar": {"1"}, "state": {""}, "device": {string(raw)}}
	status, _, body, err := c.request(ctx, http.MethodPost, com4399OAuthURL, []byte(form.Encode()))
	if err != nil {
		return fmt.Errorf("4399: OAuth 设备注册失败: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("4399: OAuth 设备注册 HTTP %d: %s", status, truncate(body, 200))
	}
	var result struct {
		Result struct {
			LoginURL string `json:"login_url"`
		} `json:"result"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("4399: OAuth 响应解析失败: %w", err)
	}
	if result.Result.LoginURL == "" {
		return fmt.Errorf("4399: OAuth 未返回 login_url: %s", result.Message)
	}
	status, _, body, err = c.follow(ctx, result.Result.LoginURL)
	if err != nil {
		return fmt.Errorf("4399: authorize 页面失败: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("4399: authorize 页面 HTTP %d", status)
	}
	c.authorize = result.Result.LoginURL
	c.form = parseHiddenForms(string(body))
	if c.form.Get("client_id") == "" || c.form.Get("state") == "" {
		return fmt.Errorf("4399: authorize 页面缺 client_id/state")
	}
	c.initial = cloneValues(c.form)
	return nil
}

func (c *Com4399Client) authorizeEndpoint(path string) string {
	u, err := url.Parse(c.authorize)
	if err != nil || u.Host == "" {
		return "https://ptlogin.4399.com" + path
	}
	return u.Scheme + "://" + u.Host + path
}

func (c *Com4399Client) request(ctx context.Context, method, rawURL string, body []byte) (int, string, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("User-Agent", com4399UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, resp.Header.Get("Location"), nil, err
	}
	return resp.StatusCode, resp.Header.Get("Location"), data, nil
}

func (c *Com4399Client) follow(ctx context.Context, rawURL string) (int, string, []byte, error) {
	for i := 0; i < 5; i++ {
		status, location, body, err := c.request(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return 0, "", nil, err
		}
		if status < 300 || status >= 400 {
			return status, location, body, nil
		}
		if location == "" {
			return status, location, body, nil
		}
		base, baseErr := url.Parse(rawURL)
		next, nextErr := url.Parse(location)
		if baseErr == nil && nextErr == nil {
			rawURL = base.ResolveReference(next).String()
		} else {
			rawURL = location
		}
	}
	return 0, "", nil, fmt.Errorf("重定向次数过多")
}

// protocolCut 是登录/授权表单段的结束标记（对齐旧实现 I4399GameSDKForm.decodeForms 的
// substringBefore 截断）。其后是忘记密码/第三方登录等模板的 hidden 值（response_type=NILL、
// auth_action=FORGET_PASSWORD、ext/extlogin_type 等）：全页提取时这些值以 last-wins 混入
// 登录表单，4399 拒绝该畸形 POST（实测 HTTP 401/202；按此截断后登录 3/3 成功）。
const protocolCut = `<label for="protocol">我已同意</label>`

func parseHiddenForms(page string) url.Values {
	if i := strings.Index(page, protocolCut); i >= 0 {
		page = page[:i]
	}
	values := url.Values{}
	for _, tag := range inputTagRE.FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, match := range inputAttrRE.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(match[1])] = html.UnescapeString(match[2])
		}
		if strings.EqualFold(attrs["type"], "hidden") && attrs["name"] != "" {
			values.Set(attrs["name"], attrs["value"])
		}
	}
	return values
}

func extractLoginErrTip(page string) string {
	match := loginTipRE.FindStringSubmatch(page)
	if len(match) == 2 {
		return strings.TrimSpace(html.UnescapeString(match[1]))
	}
	return ""
}

func (c *Com4399Client) buildSAuth(body []byte) (string, error) {
	var response struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("4399: 登录响应解析失败: %w（%s）", err, truncate(body, 300))
	}
	code := rawCode(response.Code)
	if code != "100" {
		return "", fmt.Errorf("4399: %s（code=%s）", response.Message, code)
	}
	var result struct {
		UID   json.Number `json:"uid"`
		State string      `json:"state"`
	}
	dec := json.NewDecoder(strings.NewReader(string(response.Result)))
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil || result.UID.String() == "" || result.State == "" {
		return "", fmt.Errorf("4399: 登录响应缺 uid/state")
	}
	uid := result.UID.String()
	if f, err := strconv.ParseFloat(uid, 64); err == nil {
		uid = strconv.FormatInt(int64(f), 10)
	}
	return BuildSAuth(uid, result.State, "4399com", "ad"), nil
}

func rawCode(raw json.RawMessage) string {
	s := strings.Trim(string(raw), `" `)
	if s == "" || s == "null" {
		return ""
	}
	return s
}

func cloneValues(in url.Values) url.Values {
	out := url.Values{}
	for k, values := range in {
		out[k] = append([]string(nil), values...)
	}
	return out
}
