package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Account represents a single authenticated DeepSeek account in the pool.
type Account struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	FilePath      string       `json:"file_path"`
	Data          AuthData     `json:"data"`
	CooldownUntil time.Time    `json:"cooldown_until"`
	Failures      int          `json:"failures"`
	LastUsedAt    time.Time    `json:"last_used_at"`
	Profile       *UserProfile `json:"profile,omitempty"`
}

// IsReady returns true if account has token, cookie, and is not in cooldown.
func (a *Account) IsReady() bool {
	now := time.Now()
	return a.Data.Token != "" && a.Data.Cookie != "" && a.CooldownUntil.Before(now)
}

// CooldownRemaining returns remaining cooldown duration in seconds.
func (a *Account) CooldownRemaining() int {
	now := time.Now()
	if a.CooldownUntil.After(now) {
		return int(a.CooldownUntil.Sub(now).Seconds())
	}
	return 0
}

// AccountPool manages multiple DeepSeek accounts, round-robin rotation, and cooldowns.
type AccountPool struct {
	mu              sync.Mutex
	accounts        []*Account
	roundRobin      int
	dirPath         string
	defaultAuthPath string
	cooldown        time.Duration
}

// NewAccountPool creates an AccountPool.
func NewAccountPool(dirPath string) *AccountPool {
	if dirPath == "" {
		cwd, _ := os.Getwd()
		dirPath = filepath.Join(cwd, "accounts")
	}

	pool := &AccountPool{
		dirPath:  dirPath,
		cooldown: 5 * time.Minute,
	}

	_ = pool.Reload()
	return pool
}

// SetDefaultAuthPath sets custom default auth file path.
func (p *AccountPool) SetDefaultAuthPath(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultAuthPath = path
}

// SetCooldown sets the failure cooldown duration.
func (p *AccountPool) SetCooldown(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cooldown = d
}

// Reload re-reads all auth files from the accounts directory and default auth file.
func (p *AccountPool) Reload() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.accounts = nil

	// 1. Check accounts directory
	if entries, err := os.ReadDir(p.dirPath); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}

			fullPath := filepath.Join(p.dirPath, entry.Name())
			p.loadAccountFile(fullPath, strings.TrimSuffix(entry.Name(), ".json"))
		}
	}

	// 2. Also check default auth file
	defaultFile := p.defaultAuthPath
	if defaultFile == "" {
		cwd, _ := os.Getwd()
		defaultFile = filepath.Join(cwd, "deepseek-auth.json")
	}
	if _, err := os.Stat(defaultFile); err == nil {
		alreadyAdded := false
		for _, acc := range p.accounts {
			if acc.FilePath == defaultFile {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			p.loadAccountFile(defaultFile, "default")
		}
	}

	return nil
}

func (p *AccountPool) loadAccountFile(path, name string) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var data AuthData
	if err := json.Unmarshal(bytes, &data); err != nil || data.Token == "" {
		return
	}

	data.Token = CleanToken(data.Token)
	acc := &Account{
		ID:       fmt.Sprintf("account_%d", len(p.accounts)+1),
		Name:     name,
		FilePath: path,
		Data:     data,
	}

	p.accounts = append(p.accounts, acc)
}

// AddAccount saves a new auth account to the accounts directory.
func (p *AccountPool) AddAccount(name string, data AuthData) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(p.dirPath, 0755); err != nil {
		return err
	}

	if name == "" {
		name = fmt.Sprintf("account_%d", len(p.accounts)+1)
	}

	fileName := filepath.Join(p.dirPath, fmt.Sprintf("%s.json", name))
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(fileName, raw, 0600); err != nil {
		return err
	}

	// Reload
	p.mu.Unlock()
	_ = p.Reload()
	p.mu.Lock()

	return nil
}

// RemoveAccount deletes an account from the pool and disk.
func (p *AccountPool) RemoveAccount(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, acc := range p.accounts {
		if acc.Name == name || acc.ID == name {
			_ = os.Remove(acc.FilePath)
		}
	}

	p.mu.Unlock()
	_ = p.Reload()
	p.mu.Lock()
	return nil
}

// List returns a list of all accounts and their statuses.
func (p *AccountPool) List() []*Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]*Account, len(p.accounts))
	copy(result, p.accounts)
	return result
}

// Count returns total accounts count.
func (p *AccountPool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.accounts)
}

// NextAvailable returns the next ready account via round-robin.
func (p *AccountPool) NextAvailable() *Account {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.accounts) == 0 {
		return nil
	}

	now := time.Now()
	var ready []*Account
	for _, acc := range p.accounts {
		if acc.IsReady() {
			ready = append(ready, acc)
		}
	}

	if len(ready) == 0 {
		// Fallback to least-cooldown account
		var best *Account
		for _, acc := range p.accounts {
			if best == nil || acc.CooldownUntil.Before(best.CooldownUntil) {
				best = acc
			}
		}
		if best != nil {
			best.LastUsedAt = now
		}
		return best
	}

	p.roundRobin = (p.roundRobin + 1) % len(ready)
	selected := ready[p.roundRobin]
	selected.LastUsedAt = now
	return selected
}

// MarkFailure puts an account into cooldown on 429/401/403 errors.
func (p *AccountPool) MarkFailure(acc *Account, statusCode int, retryAfterHeader string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	acc.Failures++
	cooldownDuration := p.cooldown

	if retryAfterHeader != "" {
		if secs, err := strconv.Atoi(retryAfterHeader); err == nil && secs > 0 {
			cooldownDuration = time.Duration(secs) * time.Second
		}
	}

	if statusCode == 429 {
		// Rate limit cooldown (min 3 min, max 15 min)
		if cooldownDuration < 3*time.Minute {
			cooldownDuration = 3 * time.Minute
		}
	} else if statusCode == 401 || statusCode == 403 {
		// Expired / Auth error
		cooldownDuration = 30 * time.Minute
	}

	acc.CooldownUntil = time.Now().Add(cooldownDuration)
}

// ValidateAll validates all accounts with DeepSeek API and caches their profile info.
func (p *AccountPool) ValidateAll(ctx context.Context) {
	accounts := p.List()
	for _, acc := range accounts {
		authMgr := NewAuthManager(acc.Data.Token, "")
		authMgr.data = acc.Data
		profile, err := authMgr.ValidateToken(ctx)
		if err == nil && profile != nil {
			acc.Profile = profile
		}
	}
}

// GetDirPath returns the accounts directory path.
func (p *AccountPool) GetDirPath() string {
	return p.dirPath
}

// SwitchActive sets the specified account as the active default deepseek-auth.json.
func (p *AccountPool) SwitchActive(name string) (*Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var target *Account
	for _, acc := range p.accounts {
		if strings.EqualFold(acc.Name, name) || strings.EqualFold(acc.ID, name) {
			target = acc
			break
		}
	}

	if target == nil {
		return nil, fmt.Errorf("akkaunt topilmadi: %s", name)
	}

	defaultFile := p.defaultAuthPath
	if defaultFile == "" {
		cwd, _ := os.Getwd()
		defaultFile = filepath.Join(cwd, "deepseek-auth.json")
	}
	raw, err := json.MarshalIndent(target.Data, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(defaultFile, raw, 0600); err != nil {
		return nil, fmt.Errorf("asosiy faylga yozishda xatolik: %w", err)
	}

	return target, nil
}

// GetActiveAccountName returns the name of the currently active account.
func (p *AccountPool) GetActiveAccountName() string {
	defaultFile := p.defaultAuthPath
	if defaultFile == "" {
		cwd, _ := os.Getwd()
		defaultFile = filepath.Join(cwd, "deepseek-auth.json")
	}
	bytes, err := os.ReadFile(defaultFile)
	if err != nil {
		return ""
	}

	var cur AuthData
	if err := json.Unmarshal(bytes, &cur); err != nil || cur.Token == "" {
		return ""
	}

	cleanToken := CleanToken(cur.Token)

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, acc := range p.accounts {
		if acc.Name != "default" && CleanToken(acc.Data.Token) == cleanToken {
			return acc.Name
		}
	}

	return "default"
}
