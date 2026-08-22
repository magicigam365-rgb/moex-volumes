//go:build !windows

package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	configPath := flag.String("config", "", "путь к config.json")
	dryRun := flag.Bool("dry-run", false, "только показать расчёт (не писать файлы)")
	flag.Parse()

	cfg := loadConfigOrExit(*configPath)
	if *dryRun {
		cfg.MakeBackup = false
		cfg.EasyScalpFile = "/tmp/moex_dry/AppSettings.xml"
		cfg.FSRMvsDir = "/tmp/moex_dry/MVS"
	}
	if err := runCLI(cfg, *configPath); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}
