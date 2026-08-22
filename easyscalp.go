package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reEntry  = regexp.MustCompile(`(?s)(<(?:\w+:)?)KeyValueOfstringMarketSettingsGU98z0zq([^>]*>)(.*?)(</(?:\w+:)?)KeyValueOfstringMarketSettingsGU98z0zq(>)`)
	reKey    = regexp.MustCompile(`(?s)<[^ >]+:Key>(.*?)</[^ >]+:Key>`)
	reSymID  = regexp.MustCompile(`(?s)<(?:\w+:)?SymbolID>([^<]+)</(?:\w+:)?SymbolID>`)
	reTrade  = regexp.MustCompile(`(?i)^Trade_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}_Settings\.xml$`)
)

// setLeafValue replaces the text content of <X:LocalName>...</X:LocalName> with
// a new value (prefix is captured from the tag itself).
func setLeafValue(block string, localName string, newVal string) string {
	re := regexp.MustCompile(`(?s)(<(?:\w+:)?` + regexp.QuoteMeta(localName) + `\b[^>]*>)[^<]*(</(?:\w+:)?` + regexp.QuoteMeta(localName) + `\b[^>]*>)`)
	return re.ReplaceAllString(block, `${1}`+newVal+`${2}`)
}

// updateEasyScalp writes QuoteMaxVolume / QuoteBigVolume1 / QuoteBigVolume2 /
// ClusterVolumeScaleVol for every *_MOEX_STOCK key found in AppSettings.xml:
//   QuoteMaxVolume = Vol * k_load (база);
//   QuoteBigVolume1 = Vol * k_vol1;
//   QuoteBigVolume2 = Vol * k_vol2;
//   ClusterVolumeScaleVol = Vol * k_load (синхронно с QuoteMaxVolume).
// All three coefficients are applied to Vol independently.
// Returns count of updated entries and list of keys that could not be updated.
func updateEasyScalp(filePath string, data *MarketData, cfg *Config, logf func(string, ...interface{})) (int, []string, error) {
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		logf("EasyScalp не найден (%s) — пропускаем EasyScalp", filePath)
		return 0, nil, nil
	}
	text := string(fileData)

	if cfg.MakeBackup {
		if err := backupFile(filePath, filepath.Join(filepath.Dir(filePath), "backup")); err != nil {
			logf("Ошибка резервной копии: %v", err)
		} else {
			logf("Резервная копия создана: %s", filepath.Join(filepath.Dir(filePath), "backup", filepath.Base(filePath)))
		}
	}

	updated := 0
	var lost []string

	// Work on each entry block independently to avoid mixing keys.
	offsets := reEntry.FindAllStringSubmatchIndex(text, -1)
	var blocks []struct {
		start, end int
	}
	for _, m := range offsets {
		blocks = append(blocks, struct{ start, end int }{m[0], m[1]})
	}

	for _, b := range blocks {
		block := text[b.start:b.end]
		km := reKey.FindStringSubmatch(block)
		if len(km) < 2 {
			continue
		}
		key := strings.TrimSpace(km[1])
		ticker := strings.TrimSuffix(key, "_MOEX_STOCK")
		if ticker == key {
			continue
		}
		vol, work := data.GetVolumes(ticker, false)
		if vol <= 0 {
			lost = append(lost, key)
			continue
		}
		base := applyK(vol, cfg.KLoad)
		updatedBlock := block
		updatedBlock = setLeafValue(updatedBlock, "QuoteMaxVolume", fmt.Sprintf("%d", base))
		updatedBlock = setLeafValue(updatedBlock, "QuoteBigVolume1", fmt.Sprintf("%d", applyK(vol, cfg.KVol1)))
		updatedBlock = setLeafValue(updatedBlock, "QuoteBigVolume2", fmt.Sprintf("%d", applyK(vol, cfg.KVol2)))
		updatedBlock = setLeafValue(updatedBlock, "ClusterVolumeScaleVol", fmt.Sprintf("%d", base))
		// 5 вариантов рабочего объёма (OrderSize1..5) — синхронно с TRADING First..Fifth
		for i := 0; i < 5; i++ {
			updatedBlock = setLeafValue(updatedBlock, fmt.Sprintf("OrderSize%d", i+1), fmt.Sprintf("%d", work[i]))
		}
		text = text[:b.start] + updatedBlock + text[b.end:]
		updated++
	}

	if updated > 0 {
		ensureWritable(filePath)
		if err := os.WriteFile(filePath, []byte(text), 0o644); err != nil {
			return updated, lost, fmt.Errorf("не удалось записать %s: %w", filePath, err)
		}
	}
	return updated, lost, nil
}

// updateTradeSettings scans Config/Settings/Trade_*_Settings.xml files.
// Each file corresponds to one open stakan window. The <SymbolID> element
// identifies the ticker (e.g. SBER_MOEX_STOCK). We update QuoteMaxVolume,
// QuoteBigVolume1, QuoteBigVolume2, ClusterVolumeScaleVol, OrderSize1..5.
func updateTradeSettings(appSettingsPath string, data *MarketData, cfg *Config, logf func(string, ...interface{})) (int, error) {
	settingsDir := filepath.Join(filepath.Dir(appSettingsPath), "..", "Settings")
	entries, err := os.ReadDir(settingsDir)
	if err != nil {
		logf("Папка настроек EasyScalp не найдена (%s) — пропускаем Trade-файлы", settingsDir)
		return 0, nil
	}

	updated := 0
	for _, e := range entries {
		if e.IsDir() || !reTrade.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(settingsDir, e.Name())
		fileData, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(fileData)

		sm := reSymID.FindStringSubmatch(text)
		if len(sm) < 2 {
			continue
		}
		symID := strings.TrimSpace(sm[1])
		// Extract ticker from SymbolID: "SBER_MOEX_STOCK" -> "SBER", "SiH6_MOEX_FUT" -> "SiH6"
		ticker := symID
		for _, suffix := range []string{"_MOEX_STOCK", "_MOEX_FUT"} {
			if strings.HasSuffix(ticker, suffix) {
				ticker = strings.TrimSuffix(ticker, suffix)
				break
			}
		}
		vol, work := data.GetVolumes(ticker, false)
		if vol <= 0 {
			continue
		}
		base := applyK(vol, cfg.KLoad)
		text = setLeafValue(text, "QuoteMaxVolume", fmt.Sprintf("%d", base))
		text = setLeafValue(text, "QuoteBigVolume1", fmt.Sprintf("%d", applyK(vol, cfg.KVol1)))
		text = setLeafValue(text, "QuoteBigVolume2", fmt.Sprintf("%d", applyK(vol, cfg.KVol2)))
		text = setLeafValue(text, "ClusterVolumeScaleVol", fmt.Sprintf("%d", base))
		for i := 0; i < 5; i++ {
			text = setLeafValue(text, fmt.Sprintf("OrderSize%d", i+1), fmt.Sprintf("%d", work[i]))
		}
		ensureWritable(path)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			logf("Ошибка записи %s: %v", e.Name(), err)
			continue
		}
		updated++
		logf("  Trade-settings: %s -> %s Vol=%d base=%d work=%v", e.Name(), symID, vol, base, work)
	}
	return updated, nil
}

// reDOMTemplate matches DOM template filenames: DOM_{TICKER}_{MARKET}.xml
var reDOMTemplate = regexp.MustCompile(`(?i)^DOM_([A-Z0-9_]+?)_MOEX_(STOCK|FUT)\.xml$`)

// updateDOMTemplates scans the EasyScalp Templates directory for DOM_*.xml files
// and writes QuoteMaxVolume, QuoteBigVolume1, QuoteBigVolume2, ClusterVolumeScaleVol,
// OrderSize1..5 for each matching ticker.
// The Templates directory is derived from AppSettings.xml path (../Templates).
func updateDOMTemplates(appSettingsPath string, data *MarketData, cfg *Config, logf func(string, ...interface{})) (int, error) {
	tmplDir := filepath.Join(filepath.Dir(appSettingsPath), "..", "Templates")
	entries, err := os.ReadDir(tmplDir)
	if err != nil {
		logf("Папка шаблонов EasyScalp не найдена (%s) — пропускаем DOM-шаблоны", tmplDir)
		return 0, nil
	}

	updated := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := reDOMTemplate.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		ticker := m[1]
		vol, work := data.GetVolumes(ticker, false)
		if vol <= 0 {
			continue
		}
		path := filepath.Join(tmplDir, e.Name())
		fileData, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(fileData)
		base := applyK(vol, cfg.KLoad)
		text = setLeafValue(text, "QuoteMaxVolume", fmt.Sprintf("%d", base))
		text = setLeafValue(text, "QuoteBigVolume1", fmt.Sprintf("%d", applyK(vol, cfg.KVol1)))
		text = setLeafValue(text, "QuoteBigVolume2", fmt.Sprintf("%d", applyK(vol, cfg.KVol2)))
		text = setLeafValue(text, "ClusterVolumeScaleVol", fmt.Sprintf("%d", base))
		for i := 0; i < 5; i++ {
			text = setLeafValue(text, fmt.Sprintf("OrderSize%d", i+1), fmt.Sprintf("%d", work[i]))
		}
		ensureWritable(path)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			logf("Ошибка записи шаблона %s: %v", e.Name(), err)
			continue
		}
		updated++
		logf("  DOM-шаблон: %s -> Vol=%d base=%d work=%v", e.Name(), vol, base, work)
	}
	return updated, nil
}
