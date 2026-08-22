package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PropAccount describes one prop trading account detected from EasyScalp or FSR.
type PropAccount struct {
	Name          string  // display name: "FSR Launcher", "Tinkoff Invest", etc.
	Source        string  // "fsr" or "easyscalp"
	MVSPath       string  // path to MVS folder (FSR)
	AppSettings   string  // path to AppSettings.xml (EasyScalp)
	MoneyPerPoint float64 // default money per point for this prop
}

// Known prop companies and their default install paths.
var knownProps = []struct {
	Name    string
	Paths   []string // possible MVS directories
	AppData []string // possible EasyScalp AppSettings paths
}{
	{
		Name: "FSR Launcher",
		Paths: []string{
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "FSR Launcher", "SubApps", "CS", "Data", "MVS"),
			filepath.Join(os.Getenv("PROGRAMFILES"), "FSR Launcher", "SubApps", "CS", "Data", "MVS"),
		},
	},
	{
		Name: "FSR Launcher (x64)",
		Paths: []string{
			filepath.Join(os.Getenv("PROGRAMFILES"), "FSR Launcher", "SubApps", "CS", "Data", "MVS"),
		},
	},
}

// reTradeSettings matches Trade_*_Settings.xml (EasyScalp per-window config).
var reTradeSettings = regexp.MustCompile(`(?i)^Trade_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}_Settings\.xml$`)

// DetectProps scans the system for connected prop trading programs.
// Returns a list of PropAccounts with detected paths.
func DetectProps() []PropAccount {
	var props []PropAccount

	// 1. Scan for FSR Launcher installations.
	for _, kp := range knownProps {
		for _, p := range kp.Paths {
			if dirExists(p) {
				props = append(props, PropAccount{
					Name:          kp.Name,
					Source:        "fsr",
					MVSPath:       p,
					MoneyPerPoint: 0.2, // default for FSR
				})
				break
			}
		}
	}

	// 2. Scan for EasyScalp installations in AppData.
	appData := os.Getenv("APPDATA")
	if appData != "" {
		// Common EasyScalp paths: Vataga\EasyScalp\5.1
		glob := filepath.Join(appData, "Vataga", "EasyScalp", "*", "Config", "Settings2", "AppSettings.xml")
		if matches, err := filepath.Glob(glob); err == nil {
			for _, m := range matches {
				// Extract version from path: ...\EasyScalp\5.1\...
				parts := strings.Split(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(m)))), string(os.PathSeparator))
				version := ""
				for i, p := range parts {
					if strings.ToLower(p) == "easyscalp" && i+1 < len(parts) {
						version = parts[i+1]
						break
					}
				}
				name := "EasyScalp"
				if version != "" {
					name = "EasyScalp " + version
				}
				props = append(props, PropAccount{
					Name:          name,
					Source:        "easyscalp",
					AppSettings:   m,
					MoneyPerPoint: 1.0,
				})
			}
		}
	}

	return props
}

// ScanPropsMVS reads the FSR MVS folder and returns the prefix and list of tickers.
func ScanPropsMVS(mvsPath string) (prefix string, tickers []string, err error) {
	entries, err := os.ReadDir(mvsPath)
	if err != nil {
		return "", nil, fmt.Errorf("не удалось прочитать папку %s: %w", mvsPath, err)
	}

	seen := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_Settings.tmp") {
			continue
		}
		m := rePrefixFromName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if prefix == "" {
			prefix = m[1]
		}
		ticker := ""
		if tm := reTickerFromName.FindStringSubmatch(e.Name()); len(tm) == 2 {
			ticker = tm[1]
		}
		if tm := reFutTickerFromName.FindStringSubmatch(e.Name()); len(tm) == 2 {
			ticker = tm[1]
		}
		if ticker != "" && !seen[ticker] {
			seen[ticker] = true
			tickers = append(tickers, ticker)
		}
	}
	return prefix, tickers, nil
}

// ReadMoneyPerPointFromAppSettings reads OrderSize1 ( рабочий объём ) from the
// first trade window in AppSettings.xml to detect the current money_per_point.
// This is a heuristic — returns 0 if unable to determine.
func ReadMoneyPerPointFromAppSettings(appSettingsPath string) float64 {
	data, err := os.ReadFile(appSettingsPath)
	if err != nil {
		return 0
	}
	text := string(data)

	// Find first OrderSize1 value.
	m := regexp.MustCompile(`(?s)<[^ >]+:OrderSize1>([^<]+)</[^ >]+:OrderSize1>`).FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	val := strings.TrimSpace(m[1])
	if val == "" || val == "0" {
		return 0
	}
	// Heuristic: if OrderSize1 is 50 and default workK is x1, money_per_point ≈ 1.0
	return 0
}

// ScanEasyScalpTradeFiles reads Trade_*_Settings.xml files from the Settings
// directory and returns info about each open stakan window.
type TradeWindow struct {
	FileName string
	Ticker   string // from SymbolID
	Market   string // "STOCK" or "FUT"
}

func ScanEasyScalpTradeFiles(appSettingsPath string) []TradeWindow {
	settingsDir := filepath.Join(filepath.Dir(appSettingsPath), "..", "Settings")
	entries, err := os.ReadDir(settingsDir)
	if err != nil {
		return nil
	}

	var windows []TradeWindow
	for _, e := range entries {
		if e.IsDir() || !reTrade.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(settingsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		sm := reSymID.FindStringSubmatch(string(data))
		if len(sm) < 2 {
			continue
		}
		symID := strings.TrimSpace(sm[1])
		ticker := symID
		market := "STOCK"
		for _, suffix := range []string{"_MOEX_STOCK", "_MOEX_FUT"} {
			if strings.HasSuffix(ticker, suffix) {
				ticker = strings.TrimSuffix(ticker, suffix)
				if suffix == "_MOEX_FUT" {
					market = "FUT"
				}
				break
			}
		}
		windows = append(windows, TradeWindow{
			FileName: e.Name(),
			Ticker:   ticker,
			Market:   market,
		})
	}
	return windows
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
