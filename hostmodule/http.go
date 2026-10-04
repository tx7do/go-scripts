package hostmodule

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// HTTP egress 安全默认值。
// 白名单为空 = 全部出站被拒（fail-closed）；管理员经
// SCRIPT_HTTP_ALLOWED_DOMAINS（逗号分隔，如 "hooks.slack.com,oapi.dingtalk.com"）放行域名。
const (
	defaultHTTPTimeout  = 10 * time.Second
	maxHTTPResponseBody = 1 << 20 // 1MB
	maxHTTPRequestBody  = 1 << 20 // 1MB
	defaultUserAgent    = "go-scripts-host/1.0"
	maxRedirects        = 3
)

// HTTPOptions 约束 http 模块的出站行为（跨语言共享，Lua/JS 同一套护栏）。
type HTTPOptions struct {
	// AllowedDomains 域名白名单（精确匹配子域需显式列出；空 = 全部拒绝）。
	AllowedDomains []string
	// Timeout 单请求超时（默认 10s，上限 30s）。
	Timeout time.Duration
	// MaxResponseBody 响应体上限字节（默认/上限 1MB）。
	MaxResponseBody int64
	// DenyLoopback 环回地址硬禁开关（默认 true）。
	// 即使白名单显式列出 localhost/127.0.0.1 也拒绝，防 SSRF 基线；
	// 仅测试环境（httptest 监听 127.0.0.1）显式置 false 放行。
	DenyLoopback bool
}

// EnvHTTPAllowedDomains http 出站白名单环境变量名（逗号分隔域名）。
const EnvHTTPAllowedDomains = "SCRIPT_HTTP_ALLOWED_DOMAINS"

// HTTPAllowlistFromEnv 从环境变量读取域名白名单构造 http 护栏。
// 未设置 = 空 = 全部出站拒绝（fail-closed）。
func HTTPAllowlistFromEnv() HTTPOptions {
	raw := strings.TrimSpace(os.Getenv(EnvHTTPAllowedDomains))
	opts := HTTPOptions{}
	if raw == "" {
		return opts
	}
	for _, d := range strings.Split(raw, ",") {
		if d = strings.TrimSpace(d); d != "" {
			opts.AllowedDomains = append(opts.AllowedDomains, d)
		}
	}
	return opts
}

// NormalizeHTTPOptions 填充默认值并收敛越界配置。
func NormalizeHTTPOptions(o HTTPOptions) HTTPOptions {
	if o.Timeout <= 0 || o.Timeout > 30*time.Second {
		o.Timeout = defaultHTTPTimeout
	}
	if o.MaxResponseBody <= 0 || o.MaxResponseBody > maxHTTPResponseBody {
		o.MaxResponseBody = maxHTTPResponseBody
	}
	// trim 空项并小写化
	domains := make([]string, 0, len(o.AllowedDomains))
	for _, d := range o.AllowedDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			domains = append(domains, d)
		}
	}
	o.AllowedDomains = domains
	return o
}

// CheckAllowedURL 校验 URL：仅 http(s)、host 必须精确命中白名单
// （支持 "*.example.com" 通配一级子域；裸 "example.com" 不含子域）。
func CheckAllowedURL(rawURL string, allowed []string, denyLoopback bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("only http/https schemes are allowed")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return errors.New("url host is empty")
	}
	// 环回/元数据地址硬禁（防 SSRF 基线）；DenyLoopback=false 仅为测试逃生门
	if denyLoopback {
		for _, banned := range []string{"localhost", "127.0.0.1", "0.0.0.0", "::1", "169.254.169.254"} {
			if host == banned {
				return fmt.Errorf("host %q is not allowed", host)
			}
		}
	}

	for _, pattern := range allowed {
		if pattern == host {
			return nil
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			// 仅匹配一级子域：a.example.com 命中 *.example.com；a.b.example.com 不命中
			if strings.HasSuffix(host, suffix) && !strings.Contains(strings.TrimSuffix(host, suffix), ".") {
				return nil
			}
		}
	}
	return fmt.Errorf("host %q is not in the allowed domains list", host)
}

// HTTPRequest 执行受限的出站请求（核心实现，Lua/JS 适配层共用）。
// options: method(GET默认)、headers(map)、body(string)。
// 返回: {status, body, headers(map, 全小写键)}。
func HTTPRequest(ctx context.Context, client *http.Client, rawURL, method string, headers map[string]string, body string, maxBody int64) (map[string]any, error) {
	var bodyReader io.Reader
	if body != "" {
		if int64(len(body)) > maxHTTPRequestBody {
			return nil, errors.New("request body exceeds 1MB limit")
		}
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range headers {
		// 不允许脚本伪装/覆盖防护头
		lk := strings.ToLower(k)
		if lk == "host" || lk == "user-agent" || lk == "content-length" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(respBody)) > maxBody {
		return nil, errors.New("response body exceeds size limit")
	}

	respHeaders := make(map[string]string, len(resp.Header))
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			respHeaders[strings.ToLower(k)] = vs[0]
		}
	}

	return map[string]any{
		"status":  resp.StatusCode,
		"body":    string(respBody),
		"headers": respHeaders,
	}, nil
}

// BuildHTTPClient 构造带重定向限制的出站客户端。
// 重定向每次跳转都会重新校验白名单（CheckRedirect 内），防止白名单域名 302 到内网。
func BuildHTTPClient(opts HTTPOptions) *http.Client {
	return &http.Client{
		Timeout: opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return CheckAllowedURL(req.URL.String(), opts.AllowedDomains, opts.DenyLoopback)
		},
	}
}

// errInvalid 构造脚本侧参数错误（goja 桥接后转 JS 异常）。
func errInvalid(msg string) error { return errors.New(msg) }

// ModuleHTTP 构建语言无关的 http 模块（JS 等基于 map[string]any 桥接的语言使用）。
// 与 Lua 版（go-scripts/lua/host）共用本包 NormalizeHTTPOptions/BuildHTTPClient/CheckAllowedURL/HTTPRequest 护栏：
// 白名单 fail-closed、重定向逐跳复检、超时/体积上限。
// 返回的函数 (T, error) 形态在 goja 桥接下转为 JS 异常。
func ModuleHTTP(opts HTTPOptions) ModuleDef {
	normalized := NormalizeHTTPOptions(opts)
	client := BuildHTTPClient(normalized)

	blocked := func(rawURL string) error {
		if err := CheckAllowedURL(rawURL, normalized.AllowedDomains, normalized.DenyLoopback); err != nil {
			return errInvalid("http blocked: " + err.Error())
		}
		return nil
	}

	return ModuleDef{
		Name: "http",
		Funcs: map[string]any{
			// request(url, options?) → {status, body, headers}
			"request": func(rawURL string, options ...map[string]any) (map[string]any, error) {
				if err := blocked(rawURL); err != nil {
					return nil, err
				}
				method := "GET"
				headers := map[string]string{}
				body := ""
				if len(options) > 0 && options[0] != nil {
					o := options[0]
					if m, ok := o["method"].(string); ok && m != "" {
						method = strings.ToUpper(m)
					}
					if h, ok := o["headers"].(map[string]any); ok {
						for k, v := range h {
							headers[k] = toStringValue(v)
						}
					}
					if b, ok := o["body"].(string); ok {
						body = b
					}
				}
				return HTTPRequest(context.Background(), client, rawURL, method, headers, body, normalized.MaxResponseBody)
			},
			// get(url) → {status, body, headers}
			"get": func(rawURL string) (map[string]any, error) {
				if err := blocked(rawURL); err != nil {
					return nil, err
				}
				return HTTPRequest(context.Background(), client, rawURL, http.MethodGet, nil, "", normalized.MaxResponseBody)
			},
			// post(url, body, contentType?) → {status, body, headers}
			"post": func(rawURL, body string, contentType ...string) (map[string]any, error) {
				if err := blocked(rawURL); err != nil {
					return nil, err
				}
				ct := "application/json"
				if len(contentType) > 0 && contentType[0] != "" {
					ct = contentType[0]
				}
				return HTTPRequest(context.Background(), client, rawURL, http.MethodPost,
					map[string]string{"Content-Type": ct}, body, normalized.MaxResponseBody)
			},
		},
	}
}

func toStringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
