package main

import "math"

// calcVol computes the "крупный лот" value for акции:
// Vol = ROUND(avgDailyTurnover / (LastPrice * LotSize) * 0.01, 0)
// Returns 0 (and ok=false) if data is insufficient.
func calcVol(avgTurnover float64, sec *Security) (int64, bool) {
	if sec == nil || !sec.HasData || sec.Last <= 0 || sec.LotSize <= 0 {
		return 0, false
	}
	if avgTurnover <= 0 {
		return 0, false
	}
	vol := avgTurnover / (sec.Last * float64(sec.LotSize)) * 0.01
	return int64(math.Round(vol)), true
}

// calcVolFut computes the "крупный лот" for фьючерсы: history VOLUME is given
// in контрактах, so lots = VOLUME / LotSize; Vol = ROUND(lots * 0.01).
func calcVolFut(avgVolume float64, sec *Security) (int64, bool) {
	if sec == nil || !sec.HasData || sec.LotSize <= 0 {
		return 0, false
	}
	if avgVolume <= 0 {
		return 0, false
	}
	lots := avgVolume / float64(sec.LotSize)
	vol := lots * 0.01
	return int64(math.Round(vol)), true
}

// calcWorkVol computes the рабочий объём O (в лотах) so that один пункт цены
// обходится в moneyPerPoint рублей:
//   акции:    O = moneyPerPoint / (MinStep * LotSize)
//   фьючерсы: O = moneyPerPoint / StepPrice   (StepPrice = стоимость шага за лот)
// Если стоимость шага больше moneyPerPoint (подогнать нельзя) — возвращает 0, false.
func calcWorkVol(sec *Security, moneyPerPoint float64) (float64, bool) {
	if sec == nil {
		return 0, false
	}
	var stepCost float64
	if sec.IsFutures {
		stepCost = sec.StepPrice
	} else {
		stepCost = sec.MinStep * float64(sec.LotSize)
	}
	if stepCost <= 0 || stepCost > moneyPerPoint {
		return 0, false
	}
	return moneyPerPoint / stepCost, true
}

// workValues builds First..Fifth рабочие объёмы:
//   First..Fourth = ROUND(O * cfg.WorkK[i]), ограниченные max_lots тикера;
//   Fifth = 1 (один лот), либо O, если O больше max_lots (как в книге).
// Если O не задано (шаг дороже moneyPerPoint) — все пять нулей.
func workValues(o float64, ok bool, cfg *Config, ticker string) [5]int64 {
	var out [5]int64
	if !ok {
		return out
	}
	maxLot := int64(0)
	if cfg.MaxLots != nil {
		maxLot = int64(cfg.MaxLots[ticker])
	}
	capVal := func(v int64) int64 {
		if maxLot > 0 && v > maxLot {
			return maxLot
		}
		return v
	}
	for i := 0; i < 4 && i < len(cfg.WorkK); i++ {
		out[i] = capVal(int64(math.Round(o * cfg.WorkK[i])))
	}
	out[4] = 1
	if maxLot > 0 && int64(o) > maxLot {
		out[4] = maxLot
	}
	return out
}

// applyK rounds Vol * coefficient (same as WorksheetFunction.Round).
func applyK(vol int64, k float64) int64 {
	return int64(math.Round(float64(vol) * k))
}
