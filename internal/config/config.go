package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server ServerConfig `toml:"server"`
	Tunnel TunnelConfig `toml:"tunnel"`
	Docker DockerConfig `toml:"docker"`
	Agent  AgentConfig  `toml:"agent"`
	Backup BackupConfig `toml:"backup"`
}

type ServerConfig struct {
	URL            string `toml:"url"`
	ReportingToken string `toml:"reporting_token"`
	HostID         string `toml:"host_id"`
}

type TunnelConfig struct {
	KeyPath        string `toml:"key_path"`
	ManagementHost string `toml:"management_host"`
	TunnelPort     int    `toml:"tunnel_port"`
	TunnelUser     string `toml:"tunnel_user"`
}

type DockerConfig struct {
	ContainerPrefix string `toml:"container_prefix"`
	DataDir         string `toml:"data_dir"`
}

type AgentConfig struct {
	LogLevel   string `toml:"log_level"`
	LogDir     string `toml:"log_dir"`
	AutoUpdate bool   `toml:"auto_update"`
}

type BackupConfig struct {
	Enabled  bool   `toml:"enabled"`
	Schedule string `toml:"schedule"` // cron-like: "02:00"
}

func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".apex", "agent.toml")
}

func DefaultLogDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".apex", "logs")
}

func DefaultForwardsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".apex", "forwards.conf")
}

func Load(path string) (*Config, error) {
	path = expandHome(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.applyDefaults()
	cfg.expandPaths()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.URL == "" {
		c.Server.URL = "https://app.apex.host"
	}
	if c.Tunnel.TunnelUser == "" {
		c.Tunnel.TunnelUser = "tunnel"
	}
	if c.Docker.ContainerPrefix == "" {
		c.Docker.ContainerPrefix = "apex-"
	}
	if c.Docker.DataDir == "" {
		home, _ := os.UserHomeDir()
		c.Docker.DataDir = filepath.Join(home, ".apex", "data")
	}
	if c.Agent.LogLevel == "" {
		c.Agent.LogLevel = "info"
	}
	if c.Agent.LogDir == "" {
		home, _ := os.UserHomeDir()
		c.Agent.LogDir = filepath.Join(home, ".apex", "logs")
	}
	if c.Backup.Schedule == "" {
		c.Backup.Schedule = "02:00"
	}
}

func (c *Config) expandPaths() {
	c.Tunnel.KeyPath = expandHome(c.Tunnel.KeyPath)
	c.Docker.DataDir = expandHome(c.Docker.DataDir)
	c.Agent.LogDir = expandHome(c.Agent.LogDir)
}

func (c *Config) Validate() error {
	if c.Server.HostID == "" {
		return fmt.Errorf("server.host_id is required")
	}
	if c.Server.ReportingToken == "" {
		return fmt.Errorf("server.reporting_token is required")
	}
	if c.Tunnel.KeyPath == "" {
		return fmt.Errorf("tunnel.key_path is required")
	}
	if c.Tunnel.ManagementHost == "" {
		return fmt.Errorf("tunnel.management_host is required")
	}
	if c.Tunnel.TunnelPort == 0 {
		return fmt.Errorf("tunnel.tunnel_port is required")
	}
	return nil
}

func (c *Config) Write(path string) error {
	path = expandHome(path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
