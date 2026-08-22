package main

// MarketData holds computed volumes and working volumes per ticker for both
// акции (key = SECID) and фьючерсы (key = SHORTNAME).
type MarketData struct {
	vols      map[string]int64
	work      map[string][5]int64
}

func NewMarketData() *MarketData {
	return &MarketData{
		vols: make(map[string]int64),
		work: make(map[string][5]int64),
	}
}

// SetVolumes stores both the крупный лот and the рабочие объёмы for a ticker.
func (m *MarketData) SetVolumes(ticker string, vol int64, work [5]int64) {
	m.vols[ticker] = vol
	m.work[ticker] = work
}

// GetVolumes returns the крупный лот and рабочие объёмы for a ticker.
// For фьючерсы the ticker must be the SHORTNAME (AFLT-9.26).
func (m *MarketData) GetVolumes(ticker string, _ bool) (int64, [5]int64) {
	return m.vols[ticker], m.work[ticker]
}
