package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// saveConfigJSON сериализует конфиг в JSON (без загружаемых справочников).
func saveConfigJSON(cfg *Config) ([]byte, error) {
	copy := *cfg
	copy.MaxLots = nil
	copy.FutMap = nil
	return json.MarshalIndent(&copy, "", "  ")
}

// FutRef описывает запись справочника акция↔фьючерс (лист «Списки и данные» Q/R/S).
type FutRef struct {
	Stock string `json:"stock"`
	Coeff int    `json:"coeff"`
}

type Config struct {
	Prefix           string   `json:"prefix"`            // префикс стаканов акций/MTQR (Lite Invest)
	PrefixFut        string   `json:"prefix_fut"`        // префикс стаканов фьючерсов (Whitelist)
	Days             int      `json:"days"`              // торговых дней для среднего оборота
	FSRMvsDir        string   `json:"fsr_mvs_dir"`       // папка MVS со стаканами
	EasyScalpFile    string   `json:"easyscalp_file"`    // AppSettings.xml EasyScalp
	ReferenceFile    string   `json:"reference_file"`    // справочники из книги
	KLoad            float64  `json:"k_load"`            // загрузка объёмов: DOM/FilledAt и CLUSTER 1:1
	KVol1            float64  `json:"k_vol1"`            // объёмы 1: DOM/BigAmount
	KVol2            float64  `json:"k_vol2"`            // объёмы 2: DOM/HugeAmount
	MoneyPerPoint    float64  `json:"money_per_point"`   // цена 1 пункта в рублях (1.0, 0.5, 0.2…)
	WorkK            []float64 `json:"work_k"`           // коэффициенты рабочих объёмов First..Fourth (по умолчанию 1,2,3,0.5)
	MakeBackup       bool     `json:"make_backup"`
	HTTPTimeoutSec   int      `json:"http_timeout_sec"`
	MaxConcurrentReq int      `json:"max_concurrent_req"`

	// Новые поля
	DataSource string `json:"data_source"` // "moex" или "prop" — источник данных
	PropName   string `json:"prop_name"`   // имя выбранного пропа

	// Загружаемые из reference_file справочники.
	MaxLots map[string]int     `json:"-"`
	FutMap  map[string]FutRef  `json:"-"`
}

func DefaultConfig() *Config {
	return &Config{
		Prefix:           "Lite Invest",
		PrefixFut:        "Whitelist",
		Days:             5,
		FSRMvsDir:        `C:\Program Files (x86)\FSR Launcher\SubApps\CS\Data\MVS`,
		EasyScalpFile:    filepath.Join(os.Getenv("APPDATA"), "Vataga", "EasyScalp", "5.1", "Config", "Settings2", "AppSettings.xml"),
		ReferenceFile:    "reference.json",
		KLoad:            0.5,
		KVol1:            0.5,
		KVol2:            1,
		MoneyPerPoint:    1.0,
		WorkK:            []float64{1, 2, 3, 0.5},
		MakeBackup:       true,
		HTTPTimeoutSec:   30,
		MaxConcurrentReq: 8,
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("config.json не найден (%s): %w", path, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, fmt.Errorf("ошибка config.json: %w", err)
	}
	if cfg.Days <= 0 {
		cfg.Days = 5
	}
	if cfg.MaxConcurrentReq <= 0 {
		cfg.MaxConcurrentReq = 8
	}
	if len(cfg.WorkK) < 4 {
		cfg.WorkK = []float64{1, 2, 3, 0.5}
	}
	if cfg.MoneyPerPoint <= 0 {
		cfg.MoneyPerPoint = 1.0
	}

	// Справочники из книги — рядом с config.json (или абсолютный путь).
	// Опционально: нет файла — работаем без ограничений max_lots.
	refPath := cfg.ReferenceFile
	if !filepath.IsAbs(refPath) {
		refPath = filepath.Join(filepath.Dir(path), refPath)
	}
	if err := loadReference(cfg, refPath); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func loadReference(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		cfg.MaxLots = nil
		cfg.FutMap = nil
		return nil
	}
	var ref struct {
		MaxLots map[string]int     `json:"max_lots"`
		FutMap  map[string]FutRef  `json:"fut_map"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		return fmt.Errorf("ошибка справочников (%s): %w", path, err)
	}
	cfg.MaxLots = ref.MaxLots
	cfg.FutMap = ref.FutMap
	return nil
}
