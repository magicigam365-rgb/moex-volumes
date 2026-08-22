package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// rePrefixFromName extracts the prefix from "<prefix>.XDSD.<MARKET>.<TICKER>_Settings.tmp"
// — everything before ".XDSD."
var rePrefixFromName = regexp.MustCompile(`^(.+)\.XDSD\.`)

// reMarketFromName extracts the market type (TQBR, FUT, MTQR, etc.) from
// "<prefix>.XDSD.<MARKET>.<TICKER>_Settings.tmp"
var reMarketFromName = regexp.MustCompile(`\.XDSD\.(\w+)\.`)

// detectPrefixes scans the MVS directory for *_Settings.tmp files and extracts
// the prefixes (everything before ".XDSD.") for all market types.
func detectPrefixes(dir string) (prefixStock, prefixFut string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_Settings.tmp") {
			continue
		}
		m := rePrefixFromName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		p := m[1]
		// Determine market type from filename
		mm := reMarketFromName.FindStringSubmatch(e.Name())
		market := ""
		if len(mm) >= 2 {
			market = mm[1]
		}
		switch market {
		case "TQBR", "MTQR":
			if prefixStock == "" {
				prefixStock = p
			}
		case "FUT":
			if prefixFut == "" {
				prefixFut = p
			}
		}
		if prefixStock != "" && prefixFut != "" {
			break
		}
	}
	return prefixStock, prefixFut
}

// setAttrInSection replaces Value="..." on the given element name inside a section.
// Matches <ElementName ... Value="OLD" ... /> (attribute order preserved).
func setAttrInSection(section string, localName string, newVal string) string {
	re := regexp.MustCompile(`(?s)(<(?:\w+:)?` + regexp.QuoteMeta(localName) + `\b[^>]*?\bValue=")[^"]*(")`)
	return re.ReplaceAllString(section, `${1}`+newVal+`${2}`)
}

// updateSection edits leaf elements inside the named section (<DOM> etc.).
// Matches both prefixed and non-prefixed section tags.
func updateSection(text string, sectionName string, values map[string]string) string {
	re := regexp.MustCompile(`(?s)(<(?:\w+:)?` + regexp.QuoteMeta(sectionName) + `\b[^>]*>)(.*?)(</(?:\w+:)?` + regexp.QuoteMeta(sectionName) + `\b[^>]*>)`)
	return re.ReplaceAllStringFunc(text, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		section := sub[2]
		for localName, val := range values {
			section = setAttrInSection(section, localName, val)
		}
		return sub[1] + section + sub[3]
	})
}

// ensureWritable clears the read-only attribute before writing (files copied
// from a shared folder may arrive read-only on Windows).
func ensureWritable(path string) {
	if err := os.Chmod(path, 0o644); err != nil {
		_ = err
	}
}

// domClusterValues computes the стакан-объёмы from the base крупный лот:
//   FilledAt (DOM и CLUSTER синхронно) = Vol * k_load;
//   BigAmount  = Vol * k_vol1;
//   HugeAmount = Vol * k_vol2.
// All three coefficients are applied to Vol independently.
func domClusterValues(vol int64, cfg *Config) (filled, big, huge int64) {
	filled = applyK(vol, cfg.KLoad)
	big = applyK(vol, cfg.KVol1)
	huge = applyK(vol, cfg.KVol2)
	return filled, big, huge
}

// reStakanFile matches XDSD stakan files: <prefix>.XDSD.<MARKET>.<TICKER>_Settings.tmp
var reStakanFile = regexp.MustCompile(`^(.+)\.XDSD\.(\w+)\.(.+)_Settings\.tmp$`)

// parseStakanName extracts prefix, market and ticker from a stakan filename.
// Handles both XDSD and non-XDSD formats:
//
//	XDSD:   Lite Invest.XDSD.TQBR.SBER_Settings.tmp → prefix="Lite Invest", market=TQBR, ticker=SBER
//	Direct: TRNSQD.4.FUT.MXU6_Settings.tmp → prefix=TRNSQD, market=FUT, ticker=MXU6
//	Direct: TINKD.FUTSI0926000_Settings.tmp → prefix=TINKD, market=FUT, ticker=SI0926000
//	Direct: BINAD.CCUR.BTCUSDT_Settings.tmp → prefix=BINAD, market=CCUR, ticker=BTCUSDT
//	Direct: BYBITD.linear.BTCUSDT_Settings.tmp → prefix=BYBITD, market=linear, ticker=BTCUSDT
func parseStakanName(name string) (prefix, market, ticker string, ok bool) {
	if !strings.HasSuffix(name, "_Settings.tmp") {
		return "", "", "", false
	}
	base := strings.TrimSuffix(name, "_Settings.tmp")

	// Try XDSD format first
	if m := reStakanFile.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1]), m[2], m[3], true
	}

	// Non-XDSD: split by dots, find market and ticker
	parts := strings.Split(base, ".")
	if len(parts) < 3 {
		return "", "", "", false
	}
	// Last part is ticker, second-to-last is market, everything before is prefix
	ticker = parts[len(parts)-1]
	market = parts[len(parts)-2]
	prefix = strings.Join(parts[:len(parts)-2], ".")
	if prefix == "" || market == "" || ticker == "" {
		return "", "", "", false
	}
	return prefix, market, ticker, true
}

// updateStakany scans the MVS directory for стаканы and writes DOM, CLUSTER_PANEL and TRADING sections.
// Works with any market type (TQBR, FUT, MTQR, CRYPTO, etc.) and any dispatcher (XDSD, TINKD, TRNSQD, etc.).
// Фильтрация: cfg.FSRPrefix ("" = все), cfg.FSRMarket ("" = все).
func updateStakany(dir string, cfg *Config, data *MarketData, logf func(string, ...interface{})) (int, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logf("Папка стаканов FSR не найдена (%s) — пропускаем стаканы", dir)
		return 0, nil, nil
	}

	filterPrefix := strings.TrimSpace(cfg.FSRPrefix)
	filterMarket := strings.TrimSpace(cfg.FSRMarket)

	type stakanFile struct {
		name   string
		ticker string
		market string
		fut    bool
	}
	var files []stakanFile

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, "_Settings.tmp") {
			continue
		}

		prefix, market, ticker, ok := parseStakanName(name)
		if !ok {
			continue
		}

		// Фильтр по рынку
		if filterMarket != "" && market != filterMarket {
			continue
		}
		// Фильтр по префиксу (пропу)
		if filterPrefix != "" && prefix != filterPrefix {
			continue
		}

		files = append(files, stakanFile{
			name:   name,
			ticker: ticker,
			market: market,
			fut:    market == "FUT",
		})
	}

	updated := 0
	var lost []string

	for _, f := range files {
		ticker := f.ticker
		vol, work := data.GetVolumes(ticker, f.fut)
		if vol <= 0 && work == [5]int64{} {
			lost = append(lost, ticker)
			continue
		}
		full := filepath.Join(dir, f.name)
		fileData, err := os.ReadFile(full)
		if err != nil {
			lost = append(lost, ticker)
			continue
		}
		if cfg.MakeBackup {
			if err := backupFile(full, filepath.Join(dir, "backup")); err != nil {
				logf("Ошибка резервной копии %s: %v", f.name, err)
			}
		}
		filled, big, huge := domClusterValues(vol, cfg)
		text := string(fileData)
		text = updateSection(text, "DOM", map[string]string{
			"FilledAt":  fmt.Sprintf("%d", filled),
			"BigAmount": fmt.Sprintf("%d", big),
			"HugeAmount": fmt.Sprintf("%d", huge),
		})
		text = updateSection(text, "CLUSTER_PANEL", map[string]string{
			"FilledAt": fmt.Sprintf("%d", filled),
		})
		text = updateSection(text, "TRADING", map[string]string{
			"First_WorkAmount":  fmt.Sprintf("%d", work[0]),
			"Second_WorkAmount": fmt.Sprintf("%d", work[1]),
			"Third_WorkAmount":  fmt.Sprintf("%d", work[2]),
			"Fourth_WorkAmount": fmt.Sprintf("%d", work[3]),
			"Fifth_WorkAmount":  fmt.Sprintf("%d", work[4]),
		})
		ensureWritable(full)
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			lost = append(lost, ticker)
			continue
		}
		updated++
		base := applyK(vol, cfg.KLoad)
		logf("  FSR %s: %s [%s] Vol=%d base=%d work=%v", f.market, ticker, f.name, vol, base, work)
	}
	return updated, lost, nil
}

// ScanFSRPrefixAccounts scans MVS and returns a map of prefix -> money_per_point.
// Each prefix represents a separate trading account (prop or personal).
func ScanFSRPrefixAccounts(mvsDir string) map[string]float64 {
	entries, err := os.ReadDir(mvsDir)
	if err != nil {
		return nil
	}
	result := make(map[string]float64)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_Settings.tmp") {
			continue
		}
		pfx, _, _, ok := parseStakanName(e.Name())
		if !ok {
			continue
		}
		if _, exists := result[pfx]; !exists {
			result[pfx] = 0.2 // default
		}
	}
	return result
}

// ScanFSRPrefixMarkets returns a map of prefix -> set of markets for cascading filter.
func ScanFSRPrefixMarkets(mvsDir string) map[string][]string {
	entries, err := os.ReadDir(mvsDir)
	if err != nil {
		return nil
	}
	raw := make(map[string]map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_Settings.tmp") {
			continue
		}
		pfx, mkt, _, ok := parseStakanName(e.Name())
		if !ok {
			continue
		}
		if raw[pfx] == nil {
			raw[pfx] = make(map[string]bool)
		}
		raw[pfx][mkt] = true
	}
	result := make(map[string][]string)
	for pfx, mktSet := range raw {
		var mkts []string
		for m := range mktSet {
			mkts = append(mkts, m)
		}
		sort.Strings(mkts)
		result[pfx] = mkts
	}
	return result
}

// ScanFSRMarketsGeneric returns unique market types from any stakan files in MVS.
func ScanFSRMarketsGeneric(mvsDir string) []string {
	entries, err := os.ReadDir(mvsDir)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var markets []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_Settings.tmp") {
			continue
		}
		_, mkt, _, ok := parseStakanName(e.Name())
		if ok && !seen[mkt] {
			seen[mkt] = true
			markets = append(markets, mkt)
		}
	}
	sort.Strings(markets)
	return markets
}
