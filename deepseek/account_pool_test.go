package deepseek

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountPool_RotationAndCooldown(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dsweb_test_accounts_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	pool := NewAccountPool(tempDir)
	pool.SetDefaultAuthPath(filepath.Join(tempDir, "deepseek-auth.json"))

	// 1. Add two accounts
	err = pool.AddAccount("acc1", AuthData{
		Token:  "token_one_very_long_test_value_1234567890",
		Cookie: "cookie_one",
	})
	if err != nil {
		t.Fatalf("AddAccount acc1 failed: %v", err)
	}

	err = pool.AddAccount("acc2", AuthData{
		Token:  "token_two_very_long_test_value_1234567890",
		Cookie: "cookie_two",
	})
	if err != nil {
		t.Fatalf("AddAccount acc2 failed: %v", err)
	}

	if pool.Count() != 2 {
		t.Fatalf("want 2 accounts, got %d", pool.Count())
	}

	// 2. Test round robin
	first := pool.NextAvailable()
	second := pool.NextAvailable()
	if first == nil || second == nil {
		t.Fatalf("NextAvailable returned nil")
	}
	if first.Name == second.Name {
		t.Errorf("round robin should cycle between different accounts, got %s twice", first.Name)
	}

	// 3. Mark failure / cooldown on first
	pool.MarkFailure(first, 429, "60")
	if first.IsReady() {
		t.Errorf("first account should be in cooldown")
	}

	// 4. Now NextAvailable should return second because first is in cooldown
	next := pool.NextAvailable()
	if next.Name != second.Name {
		t.Errorf("expected available account %s, got %s", second.Name, next.Name)
	}

	// 5. Switch active
	target, err := pool.SwitchActive("acc2")
	if err != nil {
		t.Fatalf("SwitchActive failed: %v", err)
	}
	if target.Name != "acc2" {
		t.Errorf("expected target name acc2, got %s", target.Name)
	}

	// Clean up deepseek-auth.json created in cwd if any
	cwd, _ := os.Getwd()
	_ = os.Remove(filepath.Join(cwd, "deepseek-auth.json.tmp"))
}
