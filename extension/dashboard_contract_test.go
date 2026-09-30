package extension

import (
	"strings"
	"testing"
)

func TestBuildDashboardContent_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	c, err := buildDashboardContent(cfg)
	if err != nil {
		t.Fatalf("buildDashboardContent: %v", err)
	}
	if c.Path != "/dashboard/trove/content" || c.MaxUploadBytes != 64<<20 || !c.PerProcessSecret || c.Signer == nil {
		t.Fatalf("content = %+v", c)
	}
}

func TestBuildDashboardContent_ConfiguredSecret(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DashboardContentSecret = strings.Repeat("s", 32)
	c, err := buildDashboardContent(cfg)
	if err != nil || c.PerProcessSecret {
		t.Fatalf("content = %+v, %v", c, err)
	}
}

func TestConfigValidate_DashboardContent(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.DashboardContentSecret = "short" },
		func(c *Config) { c.DashboardMaxUploadBytes = -1 },
		func(c *Config) { c.DashboardContentPath = "dashboard/no-slash" },
	}
	for i, mutate := range bad {
		cfg := DefaultConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("case %d: Validate accepted it", i)
		}
	}
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults do not validate: %v", err)
	}
}
