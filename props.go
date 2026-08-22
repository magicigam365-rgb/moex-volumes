package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PropAccount describes one trading account detected from EasyScalp or FSR.
type PropAccount struct {
	Name          string  // display name: "FSR Launcher", "MD264564 — EasyScalp 5.1", etc.
	Source        string  // "fsr" or "easyscalp"
	AccountID     string  // EasyScalp AccountID (empty for FSR / default)
	MVSPath       string  // path to MVS folder (FSR)
	AppSettings   string  // path to AppSettings.xml (EasyScalp)
	MoneyPerPoint float64 // default money per point for this account
}

// Known prop companies and their default install paths.
var knownProps = []struct {
	Name    string
	Paths   []string
	AppData []string
}{
	{
		Name: "FSR Launcher",
		Paths: []string{
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "FSR Launcher", "SubApps", "CS", "Data", "MVS"),
			filepath.Join(os.Getenv("PROGRAMFILES"), "FSR Launcher", "SubApps", "CS", "Data", "MVS"),
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "FSR Launcher", "SubApps", "CS", "Data", "backup", "MVS"),
			filepath.Join(os.Getenv("PROGRAMFILES"), "FSR Launcher", "SubApps", "CS", "Data", "backup", "MVS"),
		},
	},
	{
		Name: "FSR Launcher (x64)",
		Paths: []string{
			filepath.Join(os.Getenv("PROGRAMFILES"), "FSR Launcher", "SubApps", "CS", "Data", "MVS"),
			filepath.Join(os.Getenv("PROGRAMFILES"), "FSR Launcher", "SubApps", "CS", "Data", "backup", "MVS"),
		},
	},
}

// reTradeSettings matches Trade_*_Settings.xml (EasyScalp per-window config).
var reTradeSettings = regexp.MustCompile(`(?i)^Trade_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}_Settings\.xml$`)

// reOrderSize1 matches <xxx:OrderSize1>value</xxx:OrderSize1> in Trade files.
var reOrderSize1 = regexp.MustCompile(`(?s)<[^ >]+:OrderSize1>([^<]+)</[^ >]+:OrderSize1>`)

// DetectProps scans the system for connected trading accounts (props and personal).
// Each unique prefix in FSR MVS and each unique AccountID in EasyScalp becomes a separate entry.
func DetectProps() []PropAccount {
	var props []PropAccount

	// 1. Scan FSR Launcher — each prefix = separate account (prop or personal).
	seenPrefix := make(map[string]bool)
	for _, kp := range knownProps {
		for _, p := range kp.Paths {
			if !dirExists(p) {
				continue
			}
			// Scan MVS for unique prefixes
			prefixAccounts := ScanFSRPrefixAccounts(p)
			if len(prefixAccounts) == 0 {
				name := kp.Name
				if !seenPrefix[name] {
					seenPrefix[name] = true
					props = append(props, PropAccount{
						Name:          name,
						Source:        "fsr",
						MVSPath:       p,
						MoneyPerPoint: 0.2,
					})
				}
			} else {
				for prefix, mpp := range prefixAccounts {
					if seenPrefix[prefix] {
						continue
					}
					seenPrefix[prefix] = true
					props = append(props, PropAccount{
						Name:          prefix,
						Source:        "fsr",
						MVSPath:       p,
						MoneyPerPoint: mpp,
					})
				}
			}
		}
	}

	// 2. Scan for EasyScalp installations and their accounts.
	appData := os.Getenv("APPDATA")
	if appData != "" {
		glob := filepath.Join(appData, "Vataga", "EasyScalp", "*", "Config", "Settings2", "AppSettings.xml")
		if matches, err := filepath.Glob(glob); err == nil {
			for _, appSettingsPath := range matches {
				// Extract version from path
				parts := strings.Split(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(appSettingsPath)))), string(os.PathSeparator))
				version := ""
				for i, p := range parts {
					if strings.ToLower(p) == "easyscalp" && i+1 < len(parts) {
						version = parts[i+1]
						break
					}
				}

				// Scan Trade files for unique AccountIDs
				type acctInfo struct {
					mpp float64
				}
				accountMap := make(map[string]*acctInfo)
				settingsDir := filepath.Join(filepath.Dir(appSettingsPath), "..", "Settings")
				entries, err := os.ReadDir(settingsDir)
				if err == nil {
					for _, e := range entries {
						if e.IsDir() || !reTradeSettings.MatchString(e.Name()) {
							continue
						}
						data, err := os.ReadFile(filepath.Join(settingsDir, e.Name()))
						if err != nil {
							continue
						}
						text := string(data)

						am := reAccID.FindStringSubmatch(text)
						account := ""
						if len(am) >= 2 {
							account = strings.TrimSpace(am[1])
						}

						mpp := 0.0
						sm := reOrderSize1.FindStringSubmatch(text)
						if len(sm) >= 2 {
							if v, err := strconv.ParseFloat(strings.TrimSpace(sm[1]), 64); err == nil && v > 0 {
								mpp = v
							}
						}

						info, exists := accountMap[account]
						if !exists {
							info = &acctInfo{}
							accountMap[account] = info
						}
						if mpp > 0 {
							info.mpp = mpp
						}
					}
				}

				if len(accountMap) == 0 {
					name := "EasyScalp"
					if version != "" {
						name = "EasyScalp " + version
					}
					props = append(props, PropAccount{
						Name:          name,
						Source:        "easyscalp",
						AppSettings:   appSettingsPath,
						MoneyPerPoint: 1.0,
					})
				} else {
					for account, info := range accountMap {
						displayName := account
						if displayName == "" {
							displayName = "Личный"
						}
						if version != "" {
							displayName += " — EasyScalp " + version
						}
						mpp := info.mpp
						if mpp == 0 {
							mpp = 1.0
						}
						props = append(props, PropAccount{
							Name:          displayName,
							Source:        "easyscalp",
							AccountID:     account,
							AppSettings:   appSettingsPath,
							MoneyPerPoint: mpp,
						})
					}
				}
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
		pfx, _, t, ok := parseStakanName(e.Name())
		if !ok {
			continue
		}
		if prefix == "" {
			prefix = pfx
		}
		if t != "" && !seen[t] {
			seen[t] = true
			tickers = append(tickers, t)
		}
	}
	return prefix, tickers, nil
}

// ScanEasyScalpTradeFiles reads Trade_*_Settings.xml files and returns info about each window.
type TradeWindow struct {
	FileName string
	Ticker   string
	Market   string
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
		market := ""
		if i := strings.LastIndex(symID, "_MOEX_"); i > 0 {
			ticker = symID[:i]
			market = symID[i+6:]
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
