package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	issSecuritiesURL = "https://iss.moex.com/iss/engines/stock/markets/shares/boards/TQBR/securities.json?iss.meta=off&iss.only=securities,marketdata&securities.columns=SECID,LOTSIZE,MINSTEP&marketdata.columns=SECID,LAST"

	issFutSecuritiesURL = "https://iss.moex.com/iss/engines/futures/markets/forts/securities.json?iss.meta=off&iss.only=securities,marketdata&securities.columns=SECID,SHORTNAME,STEPPRICE,LOTVOLUME,MINSTEP&marketdata.columns=SECID,LAST"

	issHistoryURL = "https://iss.moex.com/iss/history/engines/stock/markets/shares/boards/TQBR/securities.json?iss.meta=off&date=%s&iss.only=history&history.columns=SECID,TRADEDATE,VALUE,CLOSE"

	issFutHistoryURL = "https://iss.moex.com/iss/history/engines/futures/markets/forts/boards/RFUD/securities.json?iss.meta=off&date=%s&iss.only=history&history.columns=SECID,TRADEDATE,VALUE,VOLUME,CLOSE"
)

type Security struct {
	Code      string // SECID
	ShortName string // фьючерсы: имя стакана (AFLT-9.26)
	LotSize   int    // акции: LOTSIZE; фьючерсы: LOTVOLUME
	MinStep   float64
	StepPrice float64 // фьючерсы: стоимость шага цены за лот (руб)
	Last      float64
	HasData   bool
	IsFutures bool
}

type historyRow struct {
	Code   string
	Date   string
	Value  float64
	Volume float64
	Close  float64
}

func fetchJSON(client *http.Client, url string, target any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MOEX %d: %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func fetchSecurities(client *http.Client) (map[string]*Security, error) {
	var raw struct {
		Securities struct {
			Columns []string          `json:"columns"`
			Data    [][]interface{}   `json:"data"`
		} `json:"securities"`
		MarketData struct {
			Columns []string        `json:"columns"`
			Data    [][]interface{} `json:"data"`
		} `json:"marketdata"`
	}
	if err := fetchJSON(client, issSecuritiesURL, &raw); err != nil {
		return nil, err
	}

	securities := make(map[string]*Security)
	for _, row := range raw.Securities.Data {
		if len(row) < 3 {
			continue
		}
		code, _ := row[0].(string)
		lot, _ := row[1].(float64)
		step, _ := row[2].(float64)
		securities[code] = &Security{Code: code, LotSize: int(lot), MinStep: step}
	}
	// marketdata columns: SECID, LAST
	for _, row := range raw.MarketData.Data {
		if len(row) < 2 {
			continue
		}
		code, _ := row[0].(string)
		if sec, ok := securities[code]; ok {
			if last, ok := row[1].(float64); ok {
				sec.Last = last
				sec.HasData = true
			}
		}
	}
	return securities, nil
}

// fetchFutSecurities loads futures (фортс). Returns securities keyed by SECID
// and an additional map SHORTNAME -> SECID for стакан matching.
func fetchFutSecurities(client *http.Client) (map[string]*Security, map[string]string, error) {
	var raw struct {
		Securities struct {
			Columns []string          `json:"columns"`
			Data    [][]interface{}   `json:"data"`
		} `json:"securities"`
		MarketData struct {
			Columns []string        `json:"columns"`
			Data    [][]interface{} `json:"data"`
		} `json:"marketdata"`
	}
	if err := fetchJSON(client, issFutSecuritiesURL, &raw); err != nil {
		return nil, nil, err
	}

	byCode := make(map[string]*Security)
	byName := make(map[string]string)
	for _, row := range raw.Securities.Data {
		if len(row) < 5 {
			continue
		}
		code, _ := row[0].(string)
		name, _ := row[1].(string)
		stepPrice, _ := row[2].(float64)
		lot, _ := row[3].(float64)
		minStep, _ := row[4].(float64)
		sec := &Security{
			Code:      code,
			ShortName: name,
			LotSize:   int(lot),
			MinStep:   minStep,
			StepPrice: stepPrice,
			IsFutures: true,
		}
		byCode[code] = sec
		if name != "" {
			byName[name] = code
		}
	}
	// marketdata columns: SECID, LAST
	for _, row := range raw.MarketData.Data {
		if len(row) < 2 {
			continue
		}
		code, _ := row[0].(string)
		if sec, ok := byCode[code]; ok {
			if last, ok := row[1].(float64); ok {
				sec.Last = last
				sec.HasData = true
			}
		}
	}
	return byCode, byName, nil
}

// fetchHistoryForDay returns daily VALUE/VOLUME per security for a single
// trading date. The history endpoint is paginated (100 rows per page).
func fetchHistoryForDay(client *http.Client, baseURL string, date string) ([]historyRow, error) {
	base := fmt.Sprintf(baseURL, date)
	var all []historyRow
	start := 0
	for {
		url := fmt.Sprintf("%s&start=%d", base, start)
		var raw struct {
			History struct {
				Columns []string        `json:"columns"`
				Data    [][]interface{} `json:"data"`
			} `json:"history"`
		}
		if err := fetchJSON(client, url, &raw); err != nil {
			return nil, err
		}
		rows := raw.History.Data
		for _, r := range rows {
			if len(r) < 3 {
				continue
			}
			code, _ := r[0].(string)
			d, _ := r[1].(string)
			val, _ := r[2].(float64)
			var vol float64
			if len(r) > 3 {
				vol, _ = r[3].(float64)
			}
			var close float64
			if len(r) > 4 {
				close, _ = r[4].(float64)
			} else if len(r) == 4 {
				close, _ = r[3].(float64)
			}
			all = append(all, historyRow{Code: code, Date: d, Value: val, Volume: vol, Close: close})
		}
		if len(rows) < 100 {
			break
		}
		start += 100
	}
	return all, nil
}

// lastTradingDays walks back from today collecting up to n days that have
// history data. Returns the list of trading dates (newest first).
func lastTradingDays(client *http.Client, baseURL string, n int) ([]string, error) {
	var days []string
	d := time.Now()
	attempts := 0
	for len(days) < n && attempts < 45 {
		date := d.Format("2006-01-02")
		rows, err := fetchHistoryForDay(client, baseURL, date)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			days = append(days, date)
		}
		d = d.AddDate(0, 0, -1)
		attempts++
	}
	return days, nil
}

// avgDailyValue returns the average of the last n daily VALUEs for a security,
// sorted by date ascending.
func avgDailyValue(rows []historyRow, n int) (float64, bool) {
	if len(rows) == 0 {
		return 0, false
	}
	cnt := n
	if cnt > len(rows) {
		cnt = len(rows)
	}
	var sum float64
	for i := len(rows) - cnt; i < len(rows); i++ {
		sum += rows[i].Value
	}
	return sum / float64(cnt), true
}

// avgDailyVolume returns the average of the last n daily VOLUMEs (контракты).
func avgDailyVolume(rows []historyRow, n int) (float64, bool) {
	if len(rows) == 0 {
		return 0, false
	}
	cnt := n
	if cnt > len(rows) {
		cnt = len(rows)
	}
	var sum float64
	for i := len(rows) - cnt; i < len(rows); i++ {
		sum += rows[i].Volume
	}
	return sum / float64(cnt), true
}

func buildHTTPClient(timeoutSec int) *http.Client {
	tr := &http.Transport{
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &http.Client{Timeout: time.Duration(timeoutSec) * time.Second, Transport: tr}
}

var _ = io.Discard
