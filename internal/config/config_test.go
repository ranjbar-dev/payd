package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func loadExample(t *testing.T) Config {
	t.Helper()
	data, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "payd.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("example config is invalid: %v", err)
	}
	return cfg
}

func TestConfigSafetyValidation(t *testing.T) {
	base := loadExample(t)
	tests := map[string]func(*Config){
		"CFG-002 invalid contract":  func(c *Config) { c.Assets[1].Contract = "not-a-tron-address" },
		"CFG-003 plaintext API key": func(c *Config) { c.Auth.APIKeys[0].KeyHash = "plaintext" },
		"CFG-007 empty consumer":    func(c *Config) { c.IPN.Consumers[0].Name = "" },
		"CFG-008 reused secret": func(c *Config) {
			c.IPN.Consumers = append(c.IPN.Consumers, Consumer{Name: "other", URL: "https://example.net/ipn", Secret: c.IPN.Consumers[0].Secret, Enabled: true})
		},
		"CFG-009 disabled default": func(c *Config) { c.IPN.Consumers[0].Enabled = false },
		"CFG-013 pool collision":   func(c *Config) { c.Resources.ResourceWalletIndex = 999 },
		"CFG-014 unverified asset": func(c *Config) { c.Assets[0].Verified = false },
		"CFG-015 duplicate host": func(c *Config) {
			c.Tron.Endpoints[1].URL = "https://API.TRONGRID.IO/other"
		},
		"PRC-001 wrong interval": func(c *Config) { c.Price.Interval = 30 * time.Second },
		"RES-001a fast tier cap": func(c *Config) { c.Resources.MaxPolledAddresses = 51 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Assets = append([]Asset(nil), base.Assets...)
			cfg.IPN.Consumers = append([]Consumer(nil), base.IPN.Consumers...)
			cfg.Tron.Endpoints = append([]Endpoint(nil), base.Tron.Endpoints...)
			cfg.Auth.APIKeys = append([]APIKey(nil), base.Auth.APIKeys...)
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("validation unexpectedly succeeded")
			}
		})
	}
}

func TestLoadRejectsUnknownFieldAndInsecureMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("unknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("unknown field error = %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "CFG-004") {
		t.Fatalf("insecure mode error = %v", err)
	}
}

func TestReloadScopeAndDisabledConsumers(t *testing.T) {
	current := loadExample(t)
	next := current
	next.Assets = append([]Asset(nil), current.Assets...)
	next.Assets[0].Decimals = 7
	if err := CheckReload(current, next); err != nil {
		t.Fatalf("reloadable asset change rejected: %v", err)
	}
	next.Server.Listen = "127.0.0.1:8081"
	if err := CheckReload(current, next); err == nil {
		t.Fatal("immutable server change accepted")
	}
	next = current
	next.IPN.Consumers = append([]Consumer(nil), current.IPN.Consumers...)
	next.IPN.Consumers[0].Enabled = false
	if got := DisabledConsumers(current, next); len(got) != 1 || got[0] != "local" {
		t.Fatalf("disabled consumers = %v", got)
	}
	if got := RemovedConsumers(current, next); len(got) != 0 {
		t.Fatalf("disabled consumer reported removed = %v", got)
	}
	next.IPN.Consumers = nil
	if got := RemovedConsumers(current, next); len(got) != 1 || got[0] != "local" {
		t.Fatalf("removed consumers = %v", got)
	}
}

func TestSecretsRedact(t *testing.T) {
	cfg := loadExample(t)
	cfg.Energy.APIKey = "sensitive"
	if got := cfg.Energy.APIKey.String(); got != "[REDACTED]" {
		t.Fatalf("secret rendered as %q", got)
	}
	if got := cfg.LogValue().String(); strings.Contains(got, "sensitive") {
		t.Fatal("config log value exposed a credential")
	}
}
