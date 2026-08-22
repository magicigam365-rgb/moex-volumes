//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

	runApp(cfg, configPath)
}

// showAbout opens the "О программе" dialog with version, changelog, and help.
func showAbout() {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s v%s\n\n", AppName, AppVersion))
	sb.WriteString("Загрузка объёмов MOEX для FSR и EasyScalp.\n\n")
	sb.WriteString("Возможности:\n")
	for _, entry := range Changelog {
		sb.WriteString(fmt.Sprintf("\nv%s (%s):\n", entry.Version, entry.Date))
		for _, c := range entry.Changes {
			sb.WriteString(fmt.Sprintf("  — %s\n", c))
		}
	}
	sb.WriteString("\nVK: https://vk.me/join/in94ilwPA5PTdJy2LI6w5MQGCCoY3U/mA4g=")

	walk.MsgBox(nil, AppName+" — Справка", sb.String(), walk.MsgBoxIconInformation)
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

	msg := fmt.Sprintf("Доступна новая версия: %s\nТекущая версия: %s\n\nЗагрузить новую версию?",
		release.TagName, AppVersion)
	result := walk.MsgBox(nil, AppName+" — Обновление", msg,
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion)

	if result != 6 {
		return ""
	}

	destPath := versionFilePath(normalizeVersion(release.TagName))
	if err := downloadUpdate(client, downloadURL, destPath); err != nil {
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

type guiFields struct {
	days   *walk.LineEdit
	kLoad  *walk.LineEdit
	kVol1  *walk.LineEdit
	kVol2  *walk.LineEdit
	money  *walk.LineEdit
	workK1 *walk.LineEdit
	workK2 *walk.LineEdit
	workK3 *walk.LineEdit
	workK4 *walk.LineEdit

	// Новые поля
	dataSource *walk.ComboBox
	propSelect *walk.ComboBox
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
	// Проверка обновлений при старте (в фоне)
	go checkAndNotifyUpdate()

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
	selectedPropIdx := 0

	dataSources := []string{"MOEX API (биржа)", "Проп-сервер (локально)"}
	selectedDS := 0

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

		// Применяем выбор проп-счёта
		if f.propSelect != nil {
			idx := f.propSelect.CurrentIndex()
			if idx >= 0 && idx < len(props) {
				prop := props[idx]
				next.FSRMvsDir = prop.MVSPath
				next.EasyScalpFile = prop.AppSettings
				if prop.MoneyPerPoint > 0 {
					next.MoneyPerPoint = prop.MoneyPerPoint
				}
				f.money.SetText(strconv.FormatFloat(next.MoneyPerPoint, 'f', -1, 64))
			}
		}

		// Применяем выбор источника данных
		if f.dataSource != nil {
			idx := f.dataSource.CurrentIndex()
			if idx == 1 {
				// Проп-сервер: используем локальные данные
				if f.propSelect != nil {
					propIdx := f.propSelect.CurrentIndex()
					if propIdx >= 0 && propIdx < len(props) {
						next.FSRMvsDir = props[propIdx].MVSPath
					}
				}
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

	grp := GroupBox{
		Title:  "Настройки",
		Layout: Grid{Columns: 3, Spacing: 6},
		Children: []Widget{
			Label{Text: "Источник данных"},
			ComboBox{
				AssignTo: &f.dataSource,
				Model:    dataSources,
				OnCurrentIndexChanged: func() {
					selectedDS = f.dataSource.CurrentIndex()
					_ = selectedDS
				},
			},
			Label{},

			Label{Text: "Проп-счёт"},
			ComboBox{
				AssignTo: &f.propSelect,
				Model:    propNames,
				OnCurrentIndexChanged: func() {
					idx := f.propSelect.CurrentIndex()
					if idx >= 0 && idx < len(props) && props[idx].MoneyPerPoint > 0 {
						f.money.SetText(strconv.FormatFloat(props[idx].MoneyPerPoint, 'f', -1, 64))
					}
					selectedPropIdx = f.propSelect.CurrentIndex()
					_ = selectedPropIdx
				},
			},
			Label{},

			Label{Text: "Торговых дней"},
			LineEdit{AssignTo: &f.days, MaxLength: 3, Text: strconv.Itoa(cfg.Days)},
			Label{},

			Label{Text: "k_load (загрузка, FilledAt)"},
			LineEdit{AssignTo: &f.kLoad, Text: fm(cfg.KLoad)},
			Label{},

			Label{Text: "k_vol1 (объёмы 1, BigAmount)"},
			LineEdit{AssignTo: &f.kVol1, Text: fm(cfg.KVol1)},
			Label{},

			Label{Text: "k_vol2 (объёмы 2, HugeAmount)"},
			LineEdit{AssignTo: &f.kVol2, Text: fm(cfg.KVol2)},
			Label{},

			Label{Text: "Цена пункта, руб (TRADING)"},
			LineEdit{AssignTo: &f.money, Text: fm(cfg.MoneyPerPoint)},
			Label{},

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

			Label{},
		},
	}

	_, err := MainWindow{
		Title:   AppName + " v" + AppVersion,
		MinSize: Size{500, 420},
		Size:    Size{560, 480},
		Layout:  VBox{MarginsZero: false, Spacing: 6},
		Children: []Widget{
			grp,
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
