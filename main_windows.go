//go:build windows

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func main() {
	configPath := ""
	cliMode := false
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-cli":
			cliMode = true
		case "-config":
			if i+1 < len(args) {
				i++
				configPath = args[i]
			}
		}
	}
	cfg := loadConfigOrExit(configPath)

	if cliMode {
		if err := runCLI(cfg, configPath); err != nil {
			fmt.Fprintln(os.Stderr, "Ошибка:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	checkAndNotifyUpdate()
	runApp(cfg, configPath)
}

// showAbout opens the "Справка" dialog with the full user guide.
// Uses a mutex to prevent opening multiple instances.
var helpMu sync.Mutex

func showAbout() {
	if !helpMu.TryLock() {
		return
	}
	walk.MsgBox(nil, helpTitle, helpText, walk.MsgBoxIconInformation)
	helpMu.Unlock()
}

// checkAndNotifyUpdate checks GitHub for updates and shows dialog if available.
// Returns the path to the downloaded new exe, or "" if no update / skipped.
func checkAndNotifyUpdate() string {
	client := newHTTPClient()
	release, downloadURL, err := checkForUpdate(client)
	if err != nil {
		return ""
	}
	if downloadURL == "" {
		return ""
	}

	msg := fmt.Sprintf("Доступна новая версия: %s\nТекущая версия: %s",
		release.TagName, AppVersion)
	result := walk.MsgBox(nil, AppName+" — Обновление", msg,
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion)

	if result != 6 { // No
		return ""
	}

	destPath := versionFilePath(normalizeVersion(release.TagName))
	err = downloadWithProgress(client, downloadURL, destPath, release.TagName)
	if err != nil {
		walk.MsgBox(nil, AppName+" — Ошибка", "Не удалось загрузить обновление:\n"+err.Error(),
			walk.MsgBoxIconError)
		return ""
	}

	walk.MsgBox(nil, AppName+" — Обновление",
		"Новая версия загружена:\n"+filepath.Base(destPath)+
			"\n\nЗакройте текущую программу и запустите новый файл.",
		walk.MsgBoxIconInformation)

	return destPath
}

// downloadWithProgress shows a progress dialog and downloads the update.
func downloadWithProgress(client *http.Client, url, destPath, version string) error {
	dlg, err := walk.NewDialog(nil)
	if err != nil {
		return downloadUpdate(client, url, destPath, nil)
	}

	dlg.SetTitle(AppName + " — Загрузка " + version)
	dlg.SetLayout(walk.NewVBoxLayout())

eterminateLabel, _ := walk.NewLabel(dlg)
eterminateLabel.SetText("Загрузка " + version + "...")

	progress, _ := walk.NewProgressBar(dlg)
	progress.SetRange(0, 1000)

	cancelBtn, _ := walk.NewPushButton(dlg)
	cancelBtn.SetText("Отмена")

	var dlErr error
	done := make(chan struct{})
	cancel := make(chan struct{})

	cancelBtn.Clicked().Attach(func() {
		close(cancel)
		dlg.Close(0)
	})

	go func() {
		dlErr = downloadUpdate(client, url, destPath, func(downloaded, total int64) {
			if total > 0 {
				pct := int(float64(downloaded) / float64(total) * 1000)
				progress.SetValue(pct)
			}
		})
		close(done)
	}()

	// Poll for completion/cancel while dialog runs
	go func() {
		select {
		case <-cancel:
		case <-done:
			dlg.Close(1)
		}
	}()

	dlg.Run()
	return dlErr
}

type guiFields struct {
	days   *walk.LineEdit
	kLoad  *walk.LineEdit
	kVol1  *walk.LineEdit
	kVol2  *walk.LineEdit
	money     *walk.LineEdit
	moneyUnit *walk.ComboBox
	workK1 *walk.LineEdit
	workK2 *walk.LineEdit
	workK3 *walk.LineEdit
	workK4 *walk.LineEdit

	// Новые поля
	propSelect *walk.ComboBox
	tradeMarket *walk.ComboBox
	fsrMarket   *walk.ComboBox
}

func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func (f *guiFields) apply(cfg *Config) error {
	days, err := parseInt(f.days.Text())
	if err != nil || days <= 0 {
		return fmt.Errorf("неверное число торговых дней: %q", f.days.Text())
	}
	cfg.Days = days

	kLoad, err := parseFloat(f.kLoad.Text())
	if err != nil || kLoad < 0 {
		return fmt.Errorf("неверный k_load: %q", f.kLoad.Text())
	}
	cfg.KLoad = kLoad

	kVol1, err := parseFloat(f.kVol1.Text())
	if err != nil || kVol1 < 0 {
		return fmt.Errorf("неверный k_vol1: %q", f.kVol1.Text())
	}
	cfg.KVol1 = kVol1

	kVol2, err := parseFloat(f.kVol2.Text())
	if err != nil || kVol2 < 0 {
		return fmt.Errorf("неверный k_vol2: %q", f.kVol2.Text())
	}
	cfg.KVol2 = kVol2

	money, err := parseFloat(f.money.Text())
	if err != nil || money <= 0 {
		return fmt.Errorf("неверная цена пункта: %q", f.money.Text())
	}
	cfg.MoneyPerPoint = money

	if f.moneyUnit != nil {
		idx := f.moneyUnit.CurrentIndex()
		if idx == 1 {
			cfg.MoneyUnit = "%"
		} else {
			cfg.MoneyUnit = "₽/$"
		}
	}

	var wk [4]float64
	values := []string{f.workK1.Text(), f.workK2.Text(), f.workK3.Text(), f.workK4.Text()}
	for i, v := range values {
		k, err := parseFloat(v)
		if err != nil || k < 0 {
			return fmt.Errorf("неверный коэффициент работы #%d: %q", i+1, v)
		}
		wk[i] = k
	}
	cfg.WorkK = []float64{wk[0], wk[1], wk[2], wk[3]}

	return nil
}

func runApp(cfg *Config, configPath string) {
	var te *walk.TextEdit
	var btnRun *walk.PushButton
	var progress *walk.ProgressBar
	var status *walk.Label
	f := &guiFields{}

	// Детект пропов
	props := DetectProps()
	propNames := make([]string, len(props))
	for i, p := range props {
		propNames[i] = p.Name
	}
	if len(propNames) == 0 {
		propNames = []string{"(пропы не найдены)"}
	}

	// Сканируем Trade-файлы для списков рынков EasyScalp
	tradeMarkets := ScanTradeMarketsGeneric(cfg.EasyScalpFile)
	if len(tradeMarkets) == 0 {
		tradeMarkets = []string{"STOCK", "FUT", "CURRENCY"}
	}

	// Сканируем MVS для списков рынков FSR
	fsrMarkets := ScanFSRMarketsGeneric(cfg.FSRMvsDir)
	if len(fsrMarkets) == 0 {
		fsrMarkets = []string{"TQBR", "FUT"}
	}

	appendLog := func(s string) {
		if te != nil {
			te.AppendText(s + "\r\n")
		}
	}

	var running bool
	start := func() {
		if running {
			return
		}
		next := *cfg
		next.MaxLots = cfg.MaxLots
		next.FutMap = cfg.FutMap
		if err := f.apply(&next); err != nil {
			walk.MsgBox(nil, AppName, "Ошибка настроек: "+err.Error(), walk.MsgBoxIconError)
			return
		}

		// Применяем выбор проп-счёта / аккаунта
		if f.propSelect != nil {
			idx := f.propSelect.CurrentIndex()
			if idx >= 0 && idx < len(props) {
				prop := props[idx]
				next.FSRMvsDir = prop.MVSPath
				next.EasyScalpFile = prop.AppSettings
				if prop.Source == "fsr" {
					next.EasyScalpAccount = ""
					next.EasyScalpMarket = ""
					if prop.Name != "FSR Launcher" && prop.Name != "FSR Launcher (x64)" {
						next.FSRPrefix = prop.Name
						next.Prefix = prop.Name
						next.PrefixFut = prop.Name
					} else {
						next.FSRPrefix = ""
						next.Prefix = ""
						next.PrefixFut = ""
					}
				}
				if prop.Source == "easyscalp" {
					next.FSRPrefix = ""
					next.FSRMarket = ""
					next.Prefix = ""
					next.PrefixFut = ""
					next.EasyScalpAccount = prop.AccountID
				}
			}
		}

		// Применяем фильтр рынка EasyScalp
		if f.tradeMarket != nil {
			idx := f.tradeMarket.CurrentIndex()
			if idx >= 0 && idx < len(tradeMarkets) {
				next.EasyScalpMarket = tradeMarkets[idx]
			}
		}

		// Применяем фильтр рынка FSR
		if f.fsrMarket != nil {
			idx := f.fsrMarket.CurrentIndex()
			if idx >= 0 && idx < len(fsrMarkets) {
				next.FSRMarket = fsrMarkets[idx]
			}
		}

		*cfg = next

		// Автосохранение настроек
		path := configPath
		if path == "" {
			path = defaultConfigPath()
		}
		if data, err := saveConfigJSON(cfg); err == nil {
			ensureWritable(path)
			os.WriteFile(path, data, 0o644)
		}

		running = true
		btnRun.SetEnabled(false)
		progress.SetValue(0)
		te.SetText("")
		status.SetText("")
		appendLog("Запуск…")

		done := make(chan error, 1)
		var steps int
		go func() {
			opts := &RunOptions{Config: cfg, ConfigPath: configPath, Target: "", Log: func(s string) {
				appendLog(s)
				steps++
				if steps > 100 {
					steps = 0
				}
				progress.SetValue(steps)
			}}
			done <- Run(opts)
		}()

		go func() {
			err := <-done
			appendLog("")
			if err != nil {
				appendLog("ОШИБКА: " + err.Error())
			} else {
				appendLog("ГОТОВО")
			}
			running = false
			btnRun.SetEnabled(true)
			progress.SetValue(100)
			status.SetText("ГОТОВО")
		}()
	}

	wk := []float64{1, 2, 3, 0.5}
	if len(cfg.WorkK) >= 4 {
		wk = cfg.WorkK[:4]
	}
	fm := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	moneyUnits := []string{"₽/$", "%"}
	if cfg.MoneyUnit == "%" {
		moneyUnits = []string{"%", "₽/$"}
	}

	grpData := GroupBox{
		Title:  "Счёт",
		Layout: Grid{Columns: 2, Spacing: 6},
		Children: []Widget{
			ComboBox{
				AssignTo: &f.propSelect,
				Model:    propNames,
				OnCurrentIndexChanged: func() {
					idx := f.propSelect.CurrentIndex()
					if idx >= 0 && idx < len(props) && props[idx].MoneyPerPoint > 0 {
						f.money.SetText(strconv.FormatFloat(props[idx].MoneyPerPoint, 'f', -1, 64))
					}
				},
			},
			Label{},
		},
	}

	grpCalc := GroupBox{
		Title:  "Параметры расчёта",
		Layout: Grid{Columns: 3, Spacing: 6},
		Children: []Widget{
			Label{Text: "Торговых дней"},
			LineEdit{AssignTo: &f.days, MaxLength: 3, Text: strconv.Itoa(cfg.Days)},
			Label{},

			Label{Text: "Заполнение"},
			LineEdit{AssignTo: &f.kLoad, Text: fm(cfg.KLoad)},
			Label{},

			Label{Text: "Объём 1"},
			LineEdit{AssignTo: &f.kVol1, Text: fm(cfg.KVol1)},
			Label{},

			Label{Text: "Объём 2"},
			LineEdit{AssignTo: &f.kVol2, Text: fm(cfg.KVol2)},
			Label{},

			Label{Text: "Цена пункта"},
			LineEdit{AssignTo: &f.money, Text: fm(cfg.MoneyPerPoint), MinSize: Size{Width: 100}},
			ComboBox{
				AssignTo: &f.moneyUnit,
				Model:    moneyUnits,
				MinSize:  Size{Width: 50},
			},

			Label{Text: "Рабочие объёмы ×1..×4"},
			Composite{
				Layout: HBox{Spacing: 4},
				Children: []Widget{
					LineEdit{AssignTo: &f.workK1, MaxLength: 6, Text: fm(wk[0])},
					LineEdit{AssignTo: &f.workK2, MaxLength: 6, Text: fm(wk[1])},
					LineEdit{AssignTo: &f.workK3, MaxLength: 6, Text: fm(wk[2])},
					LineEdit{AssignTo: &f.workK4, MaxLength: 6, Text: fm(wk[3])},
				},
			},
			Label{},
		},
	}

	grpES := GroupBox{
		Title:  "EasyScalp — рабочие объёмы",
		Layout: Grid{Columns: 3, Spacing: 6},
		Children: []Widget{
			Label{Text: "Секция рынка"},
			ComboBox{
				AssignTo: &f.tradeMarket,
				Model:    tradeMarkets,
			},
			Label{},
		},
	}

	grpFSR := GroupBox{
		Title:  "FSR — рабочие объёмы",
		Layout: Grid{Columns: 3, Spacing: 6},
		Children: []Widget{
			Label{Text: "Секция рынка"},
			ComboBox{
				AssignTo: &f.fsrMarket,
				Model:    fsrMarkets,
			},
			Label{},
		},
	}

	_, err := MainWindow{
		Title:   AppName + " v" + AppVersion,
		MinSize: Size{520, 520},
		Size:    Size{580, 580},
		Layout:  VBox{MarginsZero: false, Spacing: 4},
		Children: []Widget{
			grpData,
			grpCalc,
			grpES,
			grpFSR,
			TextEdit{AssignTo: &te, ReadOnly: true, HScroll: true, VScroll: true},
			Composite{
				Layout: HBox{MarginsZero: false, Spacing: 8},
				Children: []Widget{
					PushButton{
						AssignTo:  &btnRun,
						Text:      "Загрузить",
						OnClicked: func() { start() },
					},
					ProgressBar{AssignTo: &progress, MinValue: 0, MaxValue: 100},
					PushButton{
						Text: "Справка",
						OnClicked: func() {
							showAbout()
						},
					},
				},
			},
			LinkLabel{
				Text: `<a href="https://vk.me/join/in94ilwPA5PTdJy2LI6w5MQGCCoY3U/mA4g=">Обмен опытом MOEX — VK</a>`,
				OnLinkActivated: func(link *walk.LinkLabelLink) {
					cmd := exec.Command("cmd", "/c", "start", "", link.URL())
					cmd.Start()
				},
			},
			Label{AssignTo: &status, Text: ""},
		},
	}.Run()
	if err != nil {
		walk.MsgBox(nil, AppName, "Ошибка: "+err.Error(), walk.MsgBoxIconError)
	}
}
