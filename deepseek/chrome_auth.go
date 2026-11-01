package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ChromeAuthHelper automates Chrome browser login via DevTools Protocol (CDP).
type ChromeAuthHelper struct {
	port       int
	profileDir string
	chromePath string
}

// NewChromeAuthHelper creates a helper for extracting DeepSeek auth tokens from Chrome.
func NewChromeAuthHelper(port int, profileDir, chromePath string) *ChromeAuthHelper {
	if port <= 0 {
		port = 9334
	}
	if profileDir == "" {
		profileDir = filepath.Join(os.TempDir(), "deepseek-web-chrome-profile")
	}
	if chromePath == "" {
		chromePath = ResolveChromePath()
	}

	return &ChromeAuthHelper{
		port:       port,
		profileDir: profileDir,
		chromePath: chromePath,
	}
}

// ResolveChromePath locates Chrome / Chromium executable based on the operating system.
func ResolveChromePath() string {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}

	switch runtime.GOOS {
	case "darwin":
		candidates := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	case "windows":
		candidates := []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	case "linux":
		candidates := []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}
		for _, name := range candidates {
			if path, err := exec.LookPath(name); err == nil {
				return path
			}
		}
	}

	return ""
}

// LaunchAndExtract opens Chrome, navigates to chat.deepseek.com, and extracts auth tokens.
func (h *ChromeAuthHelper) LaunchAndExtract(ctx context.Context, onWaitMessage func(string)) (*AuthData, error) {
	if h.chromePath == "" {
		return nil, errors.New("Google Chrome topilmadi. CHROME_PATH muhit o'zgaruvchisini o'rnating yoki Chrome brauzerini o'rnating")
	}

	_ = os.MkdirAll(h.profileDir, 0755)

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", h.port)
	if !h.isDevToolsReady(endpoint) {
		args := []string{
			fmt.Sprintf("--user-data-dir=%s", h.profileDir),
			fmt.Sprintf("--remote-debugging-port=%d", h.port),
			"--use-mock-keychain",
			"--password-store=basic",
			"--disable-sync",
			"--disable-extensions",
			"--disable-component-update",
			"--disable-background-networking",
			"--disable-features=OptimizationHints,ScreenAI",
			"--no-first-run",
			"--no-default-browser-check",
			"https://chat.deepseek.com/",
		}

		cmd := exec.Command(h.chromePath, args...)
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("Chrome ishga tushmadi: %w", err)
		}
	}

	if onWaitMessage != nil {
		onWaitMessage("Chrome ochildi. Iltimos chat.deepseek.com sahifasiga kiring (Login qiling)...")
	}

	// Wait for DevTools endpoint
	if err := h.waitForDevTools(ctx, endpoint); err != nil {
		return nil, err
	}

	// Find chat.deepseek.com page target
	wsURL, err := h.findTargetWS(ctx)
	if err != nil {
		return nil, err
	}

	// Connect to CDP WebSocket
	cdp, err := newCDPClient(wsURL)
	if err != nil {
		return nil, fmt.Errorf("CDP ulanish xatosi: %w", err)
	}
	defer cdp.Close()

	// Enable CDP domains to track network requests and page state
	_, _ = cdp.Send("Runtime.enable", nil)
	_, _ = cdp.Send("Network.enable", nil)

	if onWaitMessage != nil {
		onWaitMessage("DeepSeek hisobingizga kiring. Tizim tokenni avtomatik tekshirmoqda...")
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			data, err := h.extractAndValidate(ctx, cdp)
			if err == nil && data != nil && data.Token != "" {
				return data, nil
			}
		}
	}
}

// TryExtractNow attempts a single extraction and live validation right away.
func (h *ChromeAuthHelper) TryExtractNow(ctx context.Context) (*AuthData, error) {
	wsURL, err := h.findTargetWS(ctx)
	if err != nil {
		return nil, err
	}

	cdp, err := newCDPClient(wsURL)
	if err != nil {
		return nil, err
	}
	defer cdp.Close()

	_, _ = cdp.Send("Runtime.enable", nil)
	_, _ = cdp.Send("Network.enable", nil)

	return h.extractAndValidate(ctx, cdp)
}

func (h *ChromeAuthHelper) isDevToolsReady(endpoint string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(endpoint)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (h *ChromeAuthHelper) waitForDevTools(ctx context.Context, endpoint string) error {
	for i := 0; i < 40; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if h.isDevToolsReady(endpoint) {
				return nil
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	return errors.New("Chrome DevTools ulanish portiga ulanib bo'lmadi")
}

func (h *ChromeAuthHelper) findTargetWS(ctx context.Context) (string, error) {
	targetListURL := fmt.Sprintf("http://127.0.0.1:%d/json", h.port)
	client := &http.Client{Timeout: 1 * time.Second}

	for i := 0; i < 30; i++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", targetListURL, nil)
		resp, err := client.Do(req)
		if err == nil {
			var targets []struct {
				Type                 string `json:"type"`
				URL                  string `json:"url"`
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&targets); err == nil {
				resp.Body.Close()
				for _, t := range targets {
					if t.Type == "page" && strings.Contains(t.URL, "deepseek.com") && t.WebSocketDebuggerURL != "" {
						return t.WebSocketDebuggerURL, nil
					}
				}
				for _, t := range targets {
					if t.Type == "page" && t.WebSocketDebuggerURL != "" {
						return t.WebSocketDebuggerURL, nil
					}
				}
			} else {
				resp.Body.Close()
			}
		}
		time.Sleep(300 * time.Millisecond)
	}

	return "", errors.New("Chrome sahifasi topilmadi")
}

func (h *ChromeAuthHelper) extractAndValidate(ctx context.Context, cdp *cdpClient) (*AuthData, error) {
	// 1. Evaluate localStorage & sessionStorage
	jsCode := `(() => {
		const res = {};
		try {
			for (let i = 0; i < localStorage.length; i++) {
				const k = localStorage.key(i);
				res[k] = localStorage.getItem(k);
			}
		} catch(e) {}
		try {
			for (let i = 0; i < sessionStorage.length; i++) {
				const k = sessionStorage.key(i);
				res[k] = sessionStorage.getItem(k);
			}
		} catch(e) {}
		return JSON.stringify(res);
	})()`

	resVal, err := cdp.Evaluate(jsCode)
	if err != nil {
		return nil, err
	}

	var store map[string]string
	_ = json.Unmarshal([]byte(resVal), &store)

	token := ""
	for _, key := range []string{"userToken", "token", "auth_token", "access_token", "accessToken"} {
		if v, ok := store[key]; ok && v != "" {
			cleaned := CleanToken(v)
			if len(cleaned) > 20 {
				token = cleaned
				break
			}
		}
	}

	// 2. Fallback to CDP captured authorization headers
	if token == "" {
		token = cdp.getCapturedToken()
	}

	if token == "" {
		return nil, errors.New("token hali paydo bo'lmadi (login qilinmagan)")
	}

	// 3. Fetch Cookies via Network.getAllCookies
	cookies, _ := cdp.GetAllCookies()
	var cookieParts []string
	cookieCount := 0
	for _, c := range cookies {
		if strings.Contains(c.Domain, "deepseek.com") {
			cookieParts = append(cookieParts, fmt.Sprintf("%s=%s", c.Name, c.Value))
			cookieCount++
		}
	}
	cookieStr := strings.Join(cookieParts, "; ")

	hifDliq, hifLeim := cdp.getCapturedHifHeaders()

	// 4. CRITICAL: Validate with DeepSeek API
	authMgr := &AuthManager{
		data: AuthData{
			Token:   token,
			Cookie:  cookieStr,
			HifDliq: hifDliq,
			HifLeim: hifLeim,
			BaseURL: DefaultBaseURL,
		},
		client: &http.Client{Timeout: 8 * time.Second},
	}

	profile, err := authMgr.ValidateToken(ctx)
	if err != nil || profile == nil {
		return nil, fmt.Errorf("token serverda tasdiqlanmadi: %v", err)
	}

	return &AuthData{
		Token:        token,
		Cookie:       cookieStr,
		HifDliq:      hifDliq,
		HifLeim:      hifLeim,
		BaseURL:      DefaultBaseURL,
		CookiesCount: cookieCount,
	}, nil
}

// Minimal CDP WebSocket Client
type cdpClient struct {
	ws            *websocket.Conn
	msgID         int64
	mu            sync.Mutex
	awaits        map[int64]chan []byte
	capturedToken string
	hifDliq       string
	hifLeim       string
}

func newCDPClient(wsURL string) (*cdpClient, error) {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}

	c := &cdpClient{
		ws:     conn,
		awaits: make(map[int64]chan []byte),
	}

	go c.listen()
	return c, nil
}

func (c *cdpClient) Close() {
	_ = c.ws.Close()
}

func (c *cdpClient) listen() {
	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return
		}

		var event struct {
			ID     int64                  `json:"id"`
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}

		if err := json.Unmarshal(msg, &event); err == nil {
			if event.ID > 0 {
				c.mu.Lock()
				ch, ok := c.awaits[event.ID]
				if ok {
					delete(c.awaits, event.ID)
					ch <- msg
				}
				c.mu.Unlock()
			} else if event.Method == "Network.requestWillBeSent" {
				c.inspectNetworkEvent(event.Params)
			}
		}
	}
}

func (c *cdpClient) inspectNetworkEvent(params map[string]interface{}) {
	if req, ok := params["request"].(map[string]interface{}); ok {
		if headers, ok := req["headers"].(map[string]interface{}); ok {
			c.mu.Lock()
			defer c.mu.Unlock()

			for k, v := range headers {
				lk := strings.ToLower(k)
				vStr := fmt.Sprintf("%v", v)
				if lk == "authorization" && c.capturedToken == "" {
					cleaned := CleanToken(vStr)
					if len(cleaned) > 20 {
						c.capturedToken = cleaned
					}
				}
				if lk == "x-hif-dliq" {
					c.hifDliq = vStr
				}
				if lk == "x-hif-leim" {
					c.hifLeim = vStr
				}
			}
		}
	}
}

func (c *cdpClient) getCapturedToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capturedToken
}

func (c *cdpClient) getCapturedHifHeaders() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hifDliq, c.hifLeim
}

func (c *cdpClient) Send(method string, params map[string]interface{}) ([]byte, error) {
	id := atomic.AddInt64(&c.msgID, 1)
	ch := make(chan []byte, 1)

	c.mu.Lock()
	c.awaits[id] = ch
	c.mu.Unlock()

	req := map[string]interface{}{
		"id":     id,
		"method": method,
		"params": params,
	}

	if err := c.ws.WriteJSON(req); err != nil {
		c.mu.Lock()
		delete(c.awaits, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case msg := <-ch:
		return msg, nil
	case <-time.After(10 * time.Second):
		c.mu.Lock()
		delete(c.awaits, id)
		c.mu.Unlock()
		return nil, errors.New("CDP timeout")
	}
}

func (c *cdpClient) Evaluate(expression string) (string, error) {
	raw, err := c.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}

	var res struct {
		Result struct {
			Result struct {
				Value interface{} `json:"value"`
			} `json:"result"`
		} `json:"result"`
	}

	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}

	if res.Result.Result.Value == nil {
		return "", nil
	}

	return fmt.Sprintf("%v", res.Result.Result.Value), nil
}

type cdpCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

func (c *cdpClient) GetAllCookies() ([]cdpCookie, error) {
	raw, err := c.Send("Network.getAllCookies", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var res struct {
		Result struct {
			Cookies []cdpCookie `json:"cookies"`
		} `json:"result"`
	}

	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}

	return res.Result.Cookies, nil
}
