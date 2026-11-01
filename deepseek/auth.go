package deepseek

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultBaseURL   = "https://chat.deepseek.com"
	DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
)

// AuthManager manages tokens, credentials, and user profiles.
type AuthManager struct {
	data       AuthData
	customPath string
	client     *http.Client
}

// NewAuthManager initializes an AuthManager and loads available credentials.
func NewAuthManager(tokenOverride, customPath string) *AuthManager {
	m := &AuthManager{
		customPath: customPath,
		client:     &http.Client{Timeout: 15 * time.Second},
	}

	if tokenOverride != "" {
		m.data.Token = CleanToken(tokenOverride)
		return m
	}

	// 1. Try environment variables
	m.loadEnv()

	// 2. Try loading from file
	if m.data.Token == "" {
		m.loadFromFile()
	}

	return m
}

// CleanToken removes wrappers like "Bearer ", quotes, or JSON payloads {"value": "..."} / {"userToken": "..."}.
func CleanToken(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.Trim(cleaned, `"'`)

	if strings.HasPrefix(strings.ToLower(cleaned), "bearer ") {
		cleaned = strings.TrimSpace(cleaned[7:])
	}

	// If it's a JSON object string e.g. {"value":"...", "expire":...} or {"userToken":"..."}
	if strings.HasPrefix(cleaned, "{") && strings.HasSuffix(cleaned, "}") {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(cleaned), &obj); err == nil {
			for _, k := range []string{"value", "userToken", "token", "access_token", "accessToken"} {
				if v, ok := obj[k].(string); ok {
					vClean := CleanToken(v)
					if len(vClean) > 20 && !strings.HasPrefix(vClean, "{") {
						return vClean
					}
				}
			}
			// Search any key in JSON if it looks like a token
			for _, val := range obj {
				if s, ok := val.(string); ok {
					sClean := CleanToken(s)
					if len(sClean) > 20 && !strings.HasPrefix(sClean, "{") {
						return sClean
					}
				}
			}
			// If JSON contains no valid token string (e.g. {"attempts": 1, ...}), reject it
			return ""
		}
	}

	// Regex fallback for JSON substring
	for _, field := range []string{"value", "userToken", "token", "access_token", "accessToken"} {
		re := regexp.MustCompile(`"` + field + `"\s*:\s*"([^"]{20,})"`)
		if m := re.FindStringSubmatch(cleaned); len(m) > 1 {
			return CleanToken(m[1])
		}
	}

	// If cleaned still looks like an unparsed JSON object, it's not a valid token
	if strings.HasPrefix(cleaned, "{") {
		return ""
	}

	return cleaned
}

func (m *AuthManager) loadEnv() {
	// Parse .env in current dir and parent dirs if not set in os.Environ
	loadDotEnv()

	token := os.Getenv("DEEPSEEK_TOKEN")
	if token == "" {
		token = os.Getenv("DEEPSEEK_USER_TOKEN")
	}
	if token == "" {
		token = os.Getenv("DS_TOKEN")
	}

	if token != "" {
		m.data.Token = CleanToken(token)
	}
	if cookie := os.Getenv("DEEPSEEK_COOKIE"); cookie != "" {
		m.data.Cookie = cookie
	}
	if hifDliq := os.Getenv("DEEPSEEK_HIF_DLIQ"); hifDliq != "" {
		m.data.HifDliq = hifDliq
	}
	if hifLeim := os.Getenv("DEEPSEEK_HIF_LEIM"); hifLeim != "" {
		m.data.HifLeim = hifLeim
	}
}

// ExtraAuthDirs names home-relative directories searched for an auth.json
// before the built-in candidates, so an embedding application can keep its own
// configuration location (e.g. ".mytool") without forking this package.
var ExtraAuthDirs []string

func (m *AuthManager) candidateAuthFiles() []string {
	if m.customPath != "" {
		return []string{m.customPath}
	}

	var candidates []string

	// Current directory
	cwd, err := os.Getwd()
	if err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "deepseek-auth.json"),
			filepath.Join(cwd, "auth.json"),
			filepath.Join(cwd, "FreeDeepseekAPI-EN", "deepseek-auth.json"),
		)
	}

	// Home directory
	home, err := os.UserHomeDir()
	if err == nil {
		for _, dir := range ExtraAuthDirs {
			candidates = append(candidates, filepath.Join(home, dir, "auth.json"))
		}
		candidates = append(candidates,
			filepath.Join(home, ".deepseek-web", "auth.json"),
			filepath.Join(home, ".deepseek_bridge", "auth_session.json"),
		)
	}

	return candidates
}

func (m *AuthManager) loadFromFile() {
	for _, p := range m.candidateAuthFiles() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			dataBytes, err := os.ReadFile(p)
			if err != nil {
				continue
			}

			var auth AuthData
			if err := json.Unmarshal(dataBytes, &auth); err == nil && auth.Token != "" {
				m.data = auth
				m.data.Token = CleanToken(auth.Token)
				return
			}
		}
	}
}

// GetData returns the current AuthData.
func (m *AuthManager) GetData() AuthData {
	return m.data
}

// GetToken returns the current token.
func (m *AuthManager) GetToken() string {
	return m.data.Token
}

// SetToken updates the token and optionally persists it.
func (m *AuthManager) SetToken(token string, persist bool) error {
	m.data.Token = CleanToken(token)
	if persist {
		return m.Save()
	}
	return nil
}

// SetAuthData updates all auth credentials and optionally persists them.
func (m *AuthManager) SetAuthData(data AuthData, persist bool) error {
	m.data = data
	m.data.Token = CleanToken(data.Token)
	if persist {
		return m.Save()
	}
	return nil
}

// Save persists current auth data to default config file.
func (m *AuthManager) Save() error {
	savePath := m.customPath
	if savePath == "" {
		cwd, _ := os.Getwd()
		savePath = filepath.Join(cwd, "deepseek-auth.json")
	}

	dir := filepath.Dir(savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(savePath, raw, 0600)
}

// Remove deletes the local auth file.
func (m *AuthManager) Remove() error {
	m.data = AuthData{}
	for _, p := range m.candidateAuthFiles() {
		_ = os.Remove(p)
	}
	return nil
}

// ValidateToken verifies the token against /api/v0/users/current.
func (m *AuthManager) ValidateToken(ctx context.Context) (*UserProfile, error) {
	if m.data.Token == "" {
		return nil, errors.New("no DeepSeek token configured")
	}

	baseURL := m.data.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	reqURL := fmt.Sprintf("%s/api/v0/users/current", baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.data.Token))
	req.Header.Set("User-Agent", DefaultUserAgent)
	req.Header.Set("x-client-platform", "web")
	req.Header.Set("x-client-version", "2.0.0")
	req.Header.Set("x-app-version", "2.0.0")
	req.Header.Set("Referer", "https://chat.deepseek.com/")
	req.Header.Set("Origin", "https://chat.deepseek.com")

	if m.data.Cookie != "" {
		req.Header.Set("Cookie", m.data.Cookie)
	}
	if m.data.HifDliq != "" {
		req.Header.Set("x-hif-dliq", m.data.HifDliq)
	}
	if m.data.HifLeim != "" {
		req.Header.Set("x-hif-leim", m.data.HifLeim)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serverga ulanib bo'lmadi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errors.New("DeepSeek token muddati o'tgan yoki yaroqsiz (HTTP 401/403)")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BizData map[string]interface{} `json:"biz_data"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("kutilmagan javob: %s", string(body[:min(len(body), 200)]))
	}

	if res.Code != 0 {
		return nil, fmt.Errorf("autentifikatsiya xatosi (%d): %s", res.Code, res.Msg)
	}

	biz := res.Data.BizData
	profile := &UserProfile{
		ID:      fmt.Sprintf("%v", biz["id"]),
		RawData: biz,
	}

	if email, ok := biz["email"].(string); ok {
		profile.Email = email
	}
	if mobile, ok := biz["mobile"].(string); ok {
		profile.Phone = mobile
	}
	if name, ok := biz["name"].(string); ok && name != "" {
		profile.Nickname = name
	} else if nick, ok := biz["nickname"].(string); ok {
		profile.Nickname = nick
	}
	if avatar, ok := biz["avatar_url"].(string); ok {
		profile.AvatarURL = avatar
	}

	return profile, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func loadDotEnv() {
	searchPaths := []string{".env", "../.env"}
	for _, p := range searchPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
		break
	}
}
