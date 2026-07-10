package srun

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultHost                   = "nap.cug.edu.cn"
	DefaultACID                   = "1"
	DefaultN                      = "200"
	DefaultType                   = "1"
	DefaultEnc                    = "srun_bx1"
	DefaultBaseURL                = "http://" + DefaultHost
	DefaultMaxResponseBytes int64 = 1 << 20
	maxResponseBytes        int64 = 16 << 20
	alpha                         = "LVoJPiCN2R8G90yg+hmFHuacZ1OWMnrsSTXkYpUq/3dlbfKwv6xztjI7DeBE45QA"
	standardAlpha                 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
)

var (
	ipPattern    = regexp.MustCompile(`(?:\bip\b|["']ip["'])\s*:\s*["']([^"']+)["']`)
	jsonpPattern = regexp.MustCompile(`(?s)^[A-Za-z_$][A-Za-z0-9_$.]*\s*\((.*)\)\s*;?$`)
)

type Config struct {
	BaseURL string
	// Host is retained for the original configuration API and overrides an empty or default BaseURL.
	Host             string
	ACID             string
	N                string
	Type             string
	Enc              string
	Timeout          time.Duration
	MaxResponseBytes int64
}

func DefaultConfig() Config {
	return Config{
		BaseURL:          DefaultBaseURL,
		ACID:             DefaultACID,
		N:                DefaultN,
		Type:             DefaultType,
		Enc:              DefaultEnc,
		Timeout:          8 * time.Second,
		MaxResponseBytes: DefaultMaxResponseBytes,
	}
}

type Client struct {
	config  Config
	baseURL *url.URL
	http    *http.Client
}

func NewClient(config Config) (*Client, error) {
	baseURLValue := strings.TrimSpace(config.BaseURL)
	hostValue := strings.TrimSpace(config.Host)
	if hostValue != "" && (baseURLValue == "" || baseURLValue == DefaultBaseURL) {
		baseURLValue = hostValue
	}
	if baseURLValue == "" {
		baseURLValue = DefaultBaseURL
	}
	if !strings.Contains(baseURLValue, "://") {
		baseURLValue = "http://" + baseURLValue
	}
	baseURL, err := url.Parse(baseURLValue)
	if err != nil {
		return nil, fmt.Errorf("invalid portal base URL: %w", err)
	}
	if (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, errors.New("portal base URL must use http or https and include a host")
	}
	if baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("portal base URL must not contain user info, query parameters, or a fragment")
	}
	config.BaseURL = strings.TrimRight(baseURL.String(), "/")
	config.Host = ""
	if config.ACID == "" {
		config.ACID = DefaultACID
	}
	if config.N == "" {
		config.N = DefaultN
	}
	if config.Type == "" {
		config.Type = DefaultType
	}
	if config.Enc == "" {
		config.Enc = DefaultEnc
	}
	if config.Timeout == 0 {
		config.Timeout = 8 * time.Second
	} else if config.Timeout < 0 {
		return nil, errors.New("timeout must be greater than zero")
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = DefaultMaxResponseBytes
	} else if config.MaxResponseBytes < 0 || config.MaxResponseBytes > maxResponseBytes {
		return nil, fmt.Errorf("max response size must be between 1 and %d bytes", maxResponseBytes)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	return &Client{
		config:  config,
		baseURL: baseURL,
		http: &http.Client{
			Jar:     jar,
			Timeout: config.Timeout,
		},
	}, nil
}

func (c *Client) Login(username, password, ip string) (map[string]any, error) {
	return c.LoginContext(context.Background(), username, password, ip)
}

func (c *Client) LoginContext(ctx context.Context, username, password, ip string) (map[string]any, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if username == "" {
		return nil, errors.New("username is required")
	}
	if password == "" {
		return nil, errors.New("password is required")
	}

	var err error
	if ip == "" {
		ip, err = c.DiscoverIPContext(ctx)
		if err != nil {
			return nil, err
		}
	} else if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("invalid client IP %q", ip)
	}

	callback := fmt.Sprintf("jQuery%d", nowMS())
	challengeText, err := c.get(ctx, "/cgi-bin/get_challenge", url.Values{
		"callback": []string{callback},
		"username": []string{username},
		"ip":       []string{ip},
		"_":        []string{fmt.Sprintf("%d", nowMS())},
	})
	if err != nil {
		return nil, err
	}

	challenge, err := ParseJSONP(challengeText)
	if err != nil {
		return nil, err
	}
	token, _ := challenge["challenge"].(string)
	if token == "" {
		return nil, fmt.Errorf("challenge token missing from portal response: %v", challenge)
	}

	hmd5 := HMACMD5Hex(token, password)
	info, err := Info(username, password, ip, token, c.config)
	if err != nil {
		return nil, err
	}
	chksum := Checksum(token, username, hmd5, ip, info, c.config)

	portalOS, portalName := portalPlatform()
	return c.getJSONP(ctx, "/cgi-bin/srun_portal", url.Values{
		"callback":     []string{callback},
		"action":       []string{"login"},
		"username":     []string{username},
		"password":     []string{"{MD5}" + hmd5},
		"ac_id":        []string{c.config.ACID},
		"ip":           []string{ip},
		"chksum":       []string{chksum},
		"info":         []string{info},
		"n":            []string{c.config.N},
		"type":         []string{c.config.Type},
		"os":           []string{portalOS},
		"name":         []string{portalName},
		"double_stack": []string{"0"},
		"_":            []string{fmt.Sprintf("%d", nowMS())},
	})
}

func (c *Client) DiscoverIP() (string, error) {
	return c.DiscoverIPContext(context.Background())
}

func (c *Client) DiscoverIPContext(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("context is required")
	}
	html, err := c.get(ctx, "/srun_portal_pc", url.Values{
		"ac_id": []string{c.config.ACID},
		"theme": []string{"pro"},
	})
	if err != nil {
		return "", err
	}

	match := ipPattern.FindStringSubmatch(html)
	if len(match) < 2 {
		return "", errors.New("cannot find client ip from portal page; pass --ip")
	}
	if net.ParseIP(match[1]) == nil {
		return "", fmt.Errorf("portal returned invalid client IP %q; pass --ip", match[1])
	}
	return match[1], nil
}

func (c *Client) getJSONP(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	text, err := c.get(ctx, path, query)
	if err != nil {
		return nil, err
	}
	return ParseJSONP(text)
}

func (c *Client) get(ctx context.Context, path string, query url.Values) (string, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawPath = ""
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create portal request for %s: %w", u.Path, err)
	}
	req.Header.Set("User-Agent", defaultUserAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return "", fmt.Errorf("portal request %s failed: %w", u.Path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.config.MaxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read portal response: %w", err)
	}
	if int64(len(body)) > c.config.MaxResponseBytes {
		return "", fmt.Errorf("portal response exceeds %d bytes", c.config.MaxResponseBytes)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("portal request %s returned HTTP %d", u.Path, resp.StatusCode)
	}
	return string(body), nil
}

func ParseJSONP(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "{") {
		return parseJSONObject(text)
	}

	match := jsonpPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		if len(text) > 200 {
			text = text[:200]
		}
		return nil, fmt.Errorf("unexpected response: %s", text)
	}
	return parseJSONObject(match[1])
}

func parseJSONObject(text string) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("portal response must be a JSON object")
	}
	return result, nil
}

func Info(username, password, ip, token string, config Config) (string, error) {
	if config.ACID == "" {
		config.ACID = DefaultACID
	}
	if config.Enc == "" {
		config.Enc = DefaultEnc
	}

	payload := struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IP       string `json:"ip"`
		ACID     string `json:"acid"`
		EncVer   string `json:"enc_ver"`
	}{
		Username: username,
		Password: password,
		IP:       ip,
		ACID:     config.ACID,
		EncVer:   config.Enc,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "{SRBX1}" + customBase64(xencode(encoded, []byte(token))), nil
}

func Checksum(token, username, hmd5, ip, info string, config Config) string {
	if config.ACID == "" {
		config.ACID = DefaultACID
	}
	if config.N == "" {
		config.N = DefaultN
	}
	if config.Type == "" {
		config.Type = DefaultType
	}

	chk := token + username
	chk += token + hmd5
	chk += token + config.ACID
	chk += token + ip
	chk += token + config.N
	chk += token + config.Type
	chk += token + info

	sum := sha1.Sum([]byte(chk))
	return hex.EncodeToString(sum[:])
}

func HMACMD5Hex(token, password string) string {
	mac := hmac.New(md5.New, []byte(token))
	mac.Write([]byte(password))
	return hex.EncodeToString(mac.Sum(nil))
}

func OK(result map[string]any) bool {
	sucMsg, _ := result["suc_msg"].(string)
	errorValue, _ := result["error"].(string)
	if isDefinitiveSuccess(sucMsg) || isDefinitiveSuccess(errorValue) {
		return true
	}

	errorMsg, _ := result["error_msg"].(string)
	if errorMsg != "" && errorMsg != "ok" {
		return false
	}
	return sucMsg == "ok" || errorValue == "ok"
}

func isDefinitiveSuccess(value string) bool {
	return value == "login_ok" || value == "ip_already_online_error"
}

func defaultUserAgent() string {
	switch runtime.GOOS {
	case "windows":
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 CampusLink/1"
	case "darwin":
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 CampusLink/1"
	default:
		return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 CampusLink/1"
	}
}

func portalPlatform() (string, string) {
	switch runtime.GOOS {
	case "windows":
		return "windows+10", "windows"
	case "darwin":
		return "mac", "mac"
	default:
		return "linux", "linux"
	}
}

func nowMS() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func sencode(msg []byte, includeLen bool) []uint32 {
	values := make([]uint32, 0, (len(msg)+3)/4+1)
	for i := 0; i < len(msg); i += 4 {
		var value uint32
		for j := 0; j < 4; j++ {
			if i+j < len(msg) {
				value |= uint32(msg[i+j]) << (8 * j)
			}
		}
		values = append(values, value)
	}
	if includeLen {
		values = append(values, uint32(len(msg)))
	}
	return values
}

func lencode(values []uint32) []byte {
	result := make([]byte, 0, len(values)*4)
	for _, value := range values {
		result = append(result,
			byte(value&0xff),
			byte((value>>8)&0xff),
			byte((value>>16)&0xff),
			byte((value>>24)&0xff),
		)
	}
	return result
}

func xencode(msg, key []byte) []byte {
	if len(msg) == 0 {
		return nil
	}

	v := sencode(msg, true)
	k := sencode(key, false)
	for len(k) < 4 {
		k = append(k, 0)
	}

	n := len(v) - 1
	z := v[n]
	const c uint32 = 0x9E3779B9
	q := 6 + 52/(n+1)
	var d uint32

	for q > 0 {
		q--
		d += c
		e := (d >> 2) & 3

		for p := 0; p < n; p++ {
			y := v[p+1]
			m := (z >> 5) ^ (y << 2)
			m += (y >> 3) ^ (z << 4) ^ (d ^ y)
			m += k[(p&3)^int(e)] ^ z
			v[p] += m
			z = v[p]
		}

		y := v[0]
		m := (z >> 5) ^ (y << 2)
		m += (y >> 3) ^ (z << 4) ^ (d ^ y)
		m += k[(n&3)^int(e)] ^ z
		v[n] += m
		z = v[n]
	}

	return lencode(v)
}

func customBase64(raw []byte) string {
	encoded := base64.StdEncoding.EncodeToString(raw)
	return strings.NewReplacer(base64Pairs()...).Replace(encoded)
}

func base64Pairs() []string {
	pairs := make([]string, 0, len(standardAlpha)*2)
	for i := range standardAlpha {
		pairs = append(pairs, standardAlpha[i:i+1], alpha[i:i+1])
	}
	return pairs
}
