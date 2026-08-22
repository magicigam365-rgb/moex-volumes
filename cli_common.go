package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// runCLI executes the workflow in console mode. Shared by all platforms.
func runCLI(cfg *Config, configPath string) error {
	opts := &RunOptions{
		Config:     cfg,
		ConfigPath: configPath,
		Log:        func(s string) { fmt.Println(s) },
	}
	return Run(opts)
}

func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func loadConfigOrExit(configPath string) *Config {
	if configPath == "" {
		configPath = defaultConfigPath()
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		// config.json не найден — используем дефолты, reference.json опционален
		cfg = DefaultConfig()
	}
	return cfg
}

var _ = flag.NewFlagSet
