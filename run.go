package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// VolResult holds the computed volume for a single ticker.
type VolResult struct {
	Ticker   string
	Fut      bool
	Vol      int64
	Work     [5]int64
	LotSize  int
	Last     float64
	AvgTurn  float64
	StepCost float64
}

// RunOptions collects everything needed for a run.
type RunOptions struct {
	Config     *Config
	ConfigPath string
	Log        func(string)
	// Target ограничивает, что обновлять: "es" (только EasyScalp),
	// "stakan" (только стаканы FSR) или "" (оба).
	Target string
}

func (o *RunOptions) logf(format string, args ...interface{}) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(format, args...))
	}
}

// collectStockTickers gathers акции-тикеры из стаканов TQBR и из AppSettings.xml keys.
func collectStockTickers(cfg *Config) ([]string, error) {
	set := make(map[string]bool)

	if entries, err := os.ReadDir(cfg.FSRMvsDir); err == nil {
		prefix := strings.TrimSpace(cfg.Prefix)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if prefix != "" && !strings.HasPrefix(name, prefix) {
				continue
			}
			if m := reTickerFromName.FindStringSubmatch(name); len(m) == 2 {
				set[m[1]] = true
			}
		}
	}

	if data, err := os.ReadFile(cfg.EasyScalpFile); err == nil {
		for _, m := range reKey.FindAllStringSubmatch(string(data), -1) {
			if len(m) == 2 {
				key := strings.TrimSpace(m[1])
				t := strings.TrimSuffix(key, "_MOEX_STOCK")
				if t != key {
					set[t] = true
				}
			}
		}
	}

	var out []string
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

// collectFutTickers gathers фьючерсы-тикеры (SHORTNAME) из стаканов FUT и AppSettings.xml.
func collectFutTickers(cfg *Config) ([]string, error) {
	set := make(map[string]bool)

	prefix := strings.TrimSpace(cfg.PrefixFut)
	entries, err := os.ReadDir(cfg.FSRMvsDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if prefix != "" && !strings.HasPrefix(name, prefix) {
				continue
			}
			if m := reFutTickerFromName.FindStringSubmatch(name); len(m) == 2 {
				set[m[1]] = true
			}
		}
	}

	if data, err := os.ReadFile(cfg.EasyScalpFile); err == nil {
		for _, m := range reKey.FindAllStringSubmatch(string(data), -1) {
			if len(m) == 2 {
				key := strings.TrimSpace(m[1])
				t := strings.TrimSuffix(key, "_MOEX_FUT")
				if t != key {
					set[t] = true
				}
			}
		}
	}

	var out []string
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

// Run executes the full workflow.
func Run(opts *RunOptions) error {
	cfg := opts.Config
	if cfg == nil {
		return fmt.Errorf("нет конфигурации")
	}

	client := buildHTTPClient(cfg.HTTPTimeoutSec)

	if strings.TrimSpace(cfg.Prefix) == "" || strings.TrimSpace(cfg.PrefixFut) == "" {
		pStock, pFut := detectPrefixes(cfg.FSRMvsDir)
		if strings.TrimSpace(cfg.Prefix) == "" && pStock != "" {
			cfg.Prefix = pStock
			opts.logf("Автоопределён префикс акций: %s", pStock)
		}
		if strings.TrimSpace(cfg.PrefixFut) == "" && pFut != "" {
			cfg.PrefixFut = pFut
			opts.logf("Автоопределён префикс фьючерсов: %s", pFut)
		}
	}

	opts.logf("Получаем список акций MOEX (TQBR)…")
	securities, err := fetchSecurities(client)
	if err != nil {
		return fmt.Errorf("ошибка при загрузке списка акций: %w", err)
	}
	opts.logf("Получено бумаг: %d", len(securities))

	opts.logf("Получаем список фьючерсов MOEX (фортс)…")
	futSecurities, futByName, err := fetchFutSecurities(client)
	if err != nil {
		return fmt.Errorf("ошибка при загрузке списка фьючерсов: %w", err)
	}
	opts.logf("Получено фьючерсов: %d", len(futSecurities))

	stockTickers, err := collectStockTickers(cfg)
	if err != nil {
		return err
	}
	futTickers, err := collectFutTickers(cfg)
	if err != nil {
		return err
	}
	opts.logf("Тикеров акций: %d, фьючерсов: %d", len(stockTickers), len(futTickers))

	opts.logf("Определяем последние %d торговых дней MOEX…", cfg.Days)
	days, err := lastTradingDays(client, issHistoryURL, cfg.Days)
	if err != nil {
		return fmt.Errorf("ошибка при определении торговых дней: %w", err)
	}
	if len(days) == 0 {
		return fmt.Errorf("не удалось получить торговые дни")
	}
	opts.logf("Торговые дни: %s", strings.Join(days, ", "))

	opts.logf("Загружаем обороты акций (история MOEX)…")
	history := make(map[string][]historyRow)
	for _, day := range days {
		rows, err := fetchHistoryForDay(client, issHistoryURL, day)
		if err != nil {
			return fmt.Errorf("ошибка при загрузке истории за %s: %w", day, err)
		}
		for _, h := range rows {
			if _, need := securities[h.Code]; need {
				history[h.Code] = append(history[h.Code], h)
			}
		}
	}
	opts.logf("Получено строк истории акций: %d", len(history))

	opts.logf("Загружаем обороты фьючерсов (история фортс)…")
	futHistory := make(map[string][]historyRow)
	for _, day := range days {
		rows, err := fetchHistoryForDay(client, issFutHistoryURL, day)
		if err != nil {
			return fmt.Errorf("ошибка при загрузке истории фортс за %s: %w", day, err)
		}
		for _, h := range rows {
			if _, need := futSecurities[h.Code]; need {
				futHistory[h.Code] = append(futHistory[h.Code], h)
			}
		}
	}
	opts.logf("Получено строк истории фьючерсов: %d", len(futHistory))

	filledLast := 0
	for code, sec := range securities {
		if sec.HasData && sec.Last > 0 {
			continue
		}
		rows := history[code]
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].Close > 0 {
				sec.Last = rows[i].Close
				sec.HasData = true
				filledLast++
				break
			}
		}
	}
	for code, sec := range futSecurities {
		if sec.HasData && sec.Last > 0 {
			continue
		}
		rows := futHistory[code]
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].Close > 0 {
				sec.Last = rows[i].Close
				sec.HasData = true
				filledLast++
				break
			}
		}
	}
	if filledLast > 0 {
		opts.logf("Цены закрытия подставлены для %d бумаг (рынок закрыт)", filledLast)
	}

	data := NewMarketData()
	details := make([]VolResult, 0, len(stockTickers)+len(futTickers))

	for _, t := range stockTickers {
		sec := securities[t]
		if sec == nil {
			opts.logf("  %s: нет в списке MOEX — пропуск", t)
			continue
		}
		avg, ok := avgDailyValue(history[t], cfg.Days)
		if !ok {
			opts.logf("  %s: нет истории — пропуск", t)
			continue
		}
		vol, ok := calcVol(avg, sec)
		if !ok || vol <= 0 {
			opts.logf("  %s: объём = 0 — пропуск", t)
			continue
		}
		o, oOk := calcWorkVol(sec, cfg.MoneyPerPoint)
		work := workValues(o, oOk, cfg, t)
		stepCost := sec.MinStep * float64(sec.LotSize)
		if oOk && stepCost > 0 {
			opts.logf("  %-7s Vol=%-9d шаг=%.4f×лот=%d O=%.2f -> %v", t, vol, sec.MinStep, sec.LotSize, o, work)
		} else {
			opts.logf("  %-7s Vol=%-9d шаг дороже цены пункта -> рабочие объёмы 0", t, vol)
		}
		data.SetVolumes(t, vol, work)
		details = append(details, VolResult{Ticker: t, Vol: vol, Work: work, LotSize: sec.LotSize, Last: sec.Last, AvgTurn: avg, StepCost: stepCost})
	}

	for _, name := range futTickers {
		code, ok := futByName[name]
		if !ok {
			opts.logf("  FUT %s: нет в списке фортс — пропуск", name)
			continue
		}
		sec := futSecurities[code]
		avg, ok := avgDailyVolume(futHistory[code], cfg.Days)
		if !ok {
			opts.logf("  FUT %s: нет истории — пропуск", name)
			continue
		}
		vol, ok := calcVolFut(avg, sec)
		if !ok || vol <= 0 {
			opts.logf("  FUT %s: объём = 0 — пропуск", name)
			continue
		}
		o, oOk := calcWorkVol(sec, cfg.MoneyPerPoint)
		work := workValues(o, oOk, cfg, name)
		if oOk {
			opts.logf("  FUT %-12s Vol=%-9d шаг=%.4fруб O=%.2f -> %v", name, vol, sec.StepPrice, o, work)
		} else {
			opts.logf("  FUT %-12s Vol=%-9d шаг=%.4fруб дороже цены пункта -> рабочие объёмы 0", name, vol, sec.StepPrice)
		}
		data.SetVolumes(name, vol, work)
		details = append(details, VolResult{Ticker: name, Fut: true, Vol: vol, Work: work, LotSize: sec.LotSize, Last: sec.Last, AvgTurn: avg, StepCost: sec.StepPrice})
	}

	sort.Slice(details, func(i, j int) bool { return details[i].Ticker < details[j].Ticker })
	opts.logf("Рассчитано объёмов: %d", len(details))

	if len(details) == 0 {
		return fmt.Errorf("не рассчитано ни одного объёма — нечего записывать")
	}

	updatedAny := false
	if opts.Target != "stakan" {
		opts.logf("Обновляем EasyScalp (%s)…", cfg.EasyScalpFile)
		esUpdated, esLost, err := updateEasyScalp(cfg.EasyScalpFile, data, cfg, opts.logf)
		if err != nil {
			return err
		}
		opts.logf("EasyScalp AppSettings: обновлено %d", esUpdated)
		if len(esLost) > 0 {
			opts.logf("EasyScalp не обновлены: %s", strings.Join(esLost, ", "))
		}
		domUpdated, domErr := updateDOMTemplates(cfg.EasyScalpFile, data, cfg, opts.logf)
		if domErr != nil {
			opts.logf("Ошибка DOM-шаблонов: %v", domErr)
		} else {
			opts.logf("EasyScalp DOM-шаблоны: обновлено %d", domUpdated)
		}
		tradeUpdated, tradeErr := updateTradeSettings(cfg.EasyScalpFile, data, cfg, opts.logf)
		if tradeErr != nil {
			opts.logf("Ошибка Trade-файлов: %v", tradeErr)
		} else {
			opts.logf("EasyScalp Trade-файлы: обновлено %d", tradeUpdated)
		}
		updatedAny = updatedAny || esUpdated > 0 || domUpdated > 0 || tradeUpdated > 0
	}

	if opts.Target != "es" {
		opts.logf("Обновляем стаканы FSR (%s)…", cfg.FSRMvsDir)
		stUpdated, stLost, err := updateStakany(cfg.FSRMvsDir, cfg, data, opts.logf)
		if err != nil {
			return err
		}
		opts.logf("Стаканы: обновлено %d", stUpdated)
		if len(stLost) > 0 {
			opts.logf("Стаканы не обновлены: %s", strings.Join(stLost, ", "))
		}
		updatedAny = updatedAny || stUpdated > 0
	}

	if !updatedAny {
		return fmt.Errorf("ничего не обновлено (нет EasyScalp и/или стаканов по указанным путям)")
	}
	opts.logf("Готово.")
	return nil
}
