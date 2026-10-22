// sauth 会话 JSON 构造（facts §6.1.1）：MgbSdk.GenerateSAuth 的等价实现。
// 独立于具体登录流程（4399com / 4399pc / 163 邮箱链复用）。
package netease

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// mgbSAuth 是 sauth 会话 JSON（facts §6.1.1，EntityMgbSdkSAuthJson 字段核对）。
type mgbSAuth struct {
	AimInfo        string `json:"aim_info"`
	AppChannel     string `json:"app_channel"`
	ClientLoginSn  string `json:"client_login_sn"`
	DeviceID       string `json:"deviceid"`
	GameID         string `json:"gameid"`
	GasToken       string `json:"gas_token"`
	IP             string `json:"ip"`
	LoginChannel   string `json:"login_channel"`
	Platform       string `json:"platform"`
	Realname       string `json:"realname"`
	SdkVersion     string `json:"sdk_version"`
	SdkUID         string `json:"sdkuid"`
	SessionID      string `json:"sessionid"`
	SourcePlatform string `json:"source_platform"`
	Timestamp      string `json:"timestamp"`
	Udid           string `json:"udid"`
	UserID         string `json:"userid"`
}

// BuildSAuth 构造 sauth 会话 JSON 字符串（即 uni_sauth / login-otp 所用 "cookie"）。
func BuildSAuth(sdkUID, sessionID, channel, platform string) string {
	sn := newGUID32()
	s := mgbSAuth{
		AimInfo:        `{"aim":"127.0.0.1","tz":"+0800","tzid":"","country":"CN"}`,
		AppChannel:     channel,
		ClientLoginSn:  sn,
		DeviceID:       sn,
		GameID:         "x19",
		IP:             "127.0.0.1",
		LoginChannel:   channel,
		Platform:       platform,
		Realname:       `{"realname_type":"0"}`,
		SdkVersion:     "1.0.0",
		SdkUID:         sdkUID,
		SessionID:      sessionID,
		SourcePlatform: platform,
		Udid:           newGUID32(),
	}
	out, err := json.Marshal(s)
	if err != nil {
		// 结构体序列化不会失败；防御式返回空串由调用侧报错。
		return ""
	}
	return string(out)
}

// newGUID32 生成 32 hex 随机串（等价 C# Guid.NewGuid().ToString("N")）。
func newGUID32() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
