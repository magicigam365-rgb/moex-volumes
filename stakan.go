package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// rePrefixFromName extracts the prefix from "<prefix>.XDSD.TQBR.<TICKER>_Settings.tmp"
// or "<prefix>.XDSD.FUT.<SHORTNAME>_Settings.tmp" — everything before ".XDSD."
var rePrefixFromName = regexp.MustCompile(`^(.+)\.XDSD\.(TQBR|FUT)\.`)

// detectPrefixes scans the MVS directory for *_Settings.tmp files and extracts
// the prefixes (everything before ".XDSD.") for stocks and futures.
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
		switch m[2] {
		case "TQBR":
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
var reTickerFromName = regexp.MustCompile(`\.XDSD\.TQBR\.(.+)_Settings\.tmp$`)

// reFutTickerFromName extracts the фьючерс SHORTNAME from
// "<prefix>.XDSD.FUT.<SHORTNAME>_Settings.tmp"
var reFutTickerFromName = regexp.MustCompile(`\.XDSD\.FUT\.(.+)_Settings\.tmp$`)

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

// updateStakany scans the MVS directory for стаканы акций и фьючерсов and
// writes DOM, CLUSTER_PANEL and TRADING sections.
//   акции:  "<prefix>.XDSD.TQBR.*_Settings.tmp"
//   фьючерсы: "<prefix_fut>.XDSD.FUT.*_Settings.tmp"
// Для акций тикер — SECID, для фьючерсов — SHORTNAME (AFLT-9.26).
func updateStakany(dir string, cfg *Config, data *MarketData, logf func(string, ...interface{})) (int, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logf("Папка стаканов FSR не найдена (%s) — пропускаем стаканы", dir)
		return 0, nil, nil
	}

	var files []struct {
		name   string
		ticker string
		fut    bool
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, strings.TrimSpace(cfg.Prefix)) && strings.HasSuffix(name, "_Settings.tmp") {
			if m := reTickerFromName.FindStringSubmatch(name); len(m) == 2 {
				files = append(files, struct {
					name   string
					ticker string
					fut    bool
				}{name, m[1], false})
				continue
			}
		}
		if strings.HasPrefix(name, strings.TrimSpace(cfg.PrefixFut)) && strings.HasSuffix(name, "_Settings.tmp") {
			if m := reFutTickerFromName.FindStringSubmatch(name); len(m) == 2 {
				files = append(files, struct {
					name   string
					ticker string
					fut    bool
				}{name, m[1], true})
			}
		}
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
	}
	return updated, lost, nil
}
