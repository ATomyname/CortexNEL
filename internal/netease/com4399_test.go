package netease

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type com4399RoundTripper struct {
	t             *testing.T
	seen          []string
	omitCaptchaID bool
	captchaID     string
}

func (r *com4399RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	r.seen = append(r.seen, req.Method+" "+req.URL.Path)
	status, location, contentType, content := http.StatusOK, "", "application/json", "{}"
	switch {
	case req.Method == http.MethodPost && req.URL.Host == "m.4399api.com" && req.URL.Path == "/openapiv2/oauth.html":
		content = `{"code":100,"result":{"login_url":"https://ptlogin.4399.com/start"},"message":""}`
	case req.Method == http.MethodGet && req.URL.Path == "/start":
		contentType = "text/html"
		content = `<input type="hidden" name="client_id" value="cid"><input type="hidden" name="state" value="state1"><input type="hidden" name="ref" value="ref1">`
	case req.Method == http.MethodPost && req.URL.Path == "/oauth2/authorize.do":
		form, _ := url.ParseQuery(string(body))
		if form.Get("auth_action") != "ORILOGIN" {
			r.t.Errorf("authorize auth_action=%q", form.Get("auth_action"))
		}
		contentType = "text/html"
		content = `<input type="hidden" name="client_id" value="cid"><input type="hidden" name="state" value="state1">`
		if !r.omitCaptchaID {
			content += `<input type="hidden" name="captcha_id" value="server-captcha">`
		}
	case req.Method == http.MethodGet && req.URL.Path == "/ptlogin/captcha.do":
		r.captchaID = req.URL.Query().Get("captchaId")
		if r.omitCaptchaID {
			if len(r.captchaID) != 96 {
				r.t.Errorf("generated captcha id length=%d", len(r.captchaID))
			}
		} else if r.captchaID != "server-captcha" {
			r.t.Errorf("captcha id=%q", r.captchaID)
		}
		contentType = "image/png"
		content = "PNG"
	case req.Method == http.MethodPost && req.URL.Path == "/oauth2/loginAndAuthorize.do":
		form, _ := url.ParseQuery(string(body))
		for key, want := range map[string]string{"username": "alice", "password": "secret", "captcha_id": r.captchaID, "captcha": "ABCD"} {
			if form.Get(key) != want {
				r.t.Errorf("login %s=%q, want %q", key, form.Get(key), want)
			}
		}
		status, location, content = http.StatusFound, "https://m.4399api.com/callback", ""
	case req.Method == http.MethodGet && req.URL.Path == "/callback":
		content = `{"code":100,"result":{"uid":123456,"state":"session-state"},"message":"ok"}`
	default:
		r.t.Fatalf("unexpected request %s %s", req.Method, req.URL.String())
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(content)),
		Request:    req,
		Header:     http.Header{"Location": []string{location}, "Content-Type": []string{contentType}},
	}, nil
}

func TestCom4399OAuthUsesServerCaptchaAndRedirect(t *testing.T) {
	rt := &com4399RoundTripper{t: t}
	c := &Com4399Client{HTTP: &http.Client{Transport: rt, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	id, image, err := c.CaptchaChallenge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != "server-captcha" || string(image) != "PNG" {
		t.Fatalf("challenge=(%q,%q)", id, image)
	}
	sauth, err := c.Login(context.Background(), "alice", "secret", id, "ABCD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sauth, `"sdkuid":"123456"`) || !strings.Contains(sauth, `"sessionid":"session-state"`) {
		t.Fatalf("unexpected sauth: %s", sauth)
	}
	want := []string{"POST /openapiv2/oauth.html", "GET /start", "POST /oauth2/authorize.do", "GET /ptlogin/captcha.do", "POST /oauth2/loginAndAuthorize.do", "GET /callback"}
	if strings.Join(rt.seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("request order:\n%s", strings.Join(rt.seen, "\n"))
	}
}

func TestExtractCaptchaIDAcceptsCamelCaseAndNonHiddenMarkup(t *testing.T) {
	for _, tc := range []struct{ page, want string }{
		{`<input name="captchaId" value="camel-id">`, "camel-id"},
		{`<script>var captcha_id = "script-id";</script>`, "script-id"},
	} {
		if got := extractCaptchaID(tc.page, url.Values{}); got != tc.want {
			t.Fatalf("captcha ID = %q, want %q", got, tc.want)
		}
	}
	if got := extractCaptchaID("", url.Values{"captchaId": {"form-id"}}); got != "form-id" {
		t.Fatalf("form captcha ID = %q", got)
	}
}

func TestCom4399OAuthGeneratesCaptchaIDWhenAuthorizeOmitsIt(t *testing.T) {
	rt := &com4399RoundTripper{t: t, omitCaptchaID: true}
	c := &Com4399Client{HTTP: &http.Client{Transport: rt, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	id, image, err := c.CaptchaChallenge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 96 || id != rt.captchaID || string(image) != "PNG" {
		t.Fatalf("challenge id length=%d image=%q requestID=%q", len(id), image, rt.captchaID)
	}
}

func TestParseHiddenFormsCutsAtProtocolLabel(t *testing.T) {
	// 「我已同意」标签之后是忘记密码/第三方登录等模板的 hidden 值：全页提取会以
	// last-wins 污染登录表单（response_type=NILL、auth_action=FORGET_PASSWORD、
	// ext/extlogin_type），4399 拒绝畸形 POST（实测 HTTP 401/202）。
	page := `<form><input type="hidden" name="auth_action" value="ORILOGIN">` +
		`<input type="hidden" name="response_type" value="TOKEN">` +
		`<label for="protocol">我已同意</label>` +
		`<input type="hidden" name="auth_action" value="FORGET_PASSWORD">` +
		`<input type="hidden" name="response_type" value="NILL">` +
		`<input type="hidden" name="ext" value="0"></form>`
	values := parseHiddenForms(page)
	if got := values.Get("auth_action"); got != "ORILOGIN" {
		t.Fatalf("auth_action = %q, want ORILOGIN", got)
	}
	if got := values.Get("response_type"); got != "TOKEN" {
		t.Fatalf("response_type = %q, want TOKEN", got)
	}
	if _, ok := values["ext"]; ok {
		t.Fatalf("ext 应随截断丢弃, got %q", values.Get("ext"))
	}

	// 无协议标签时整页提取（substringBefore 语义）。
	all := parseHiddenForms(`<input type="hidden" name="state" value="abc">`)
	if got := all.Get("state"); got != "abc" {
		t.Fatalf("state = %q, want abc", got)
	}
}
