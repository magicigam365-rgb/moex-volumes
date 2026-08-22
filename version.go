package main

const (
	AppVersion = "1.1"
	AppName    = "MOEX Volumes"
)

// GitHub repo for updates (owner/repo).
const (
	GitHubOwner = "magicigam365-rgb"
	GitHubRepo  = "moex-volumes"
)

var Changelog = []VersionEntry{
	{
		Version: "1.1",
		Date:    "2026-08-22",
		Changes: []string{
			"Встроенная справка — кнопка «Справка» с инструкцией",
			"Автообновление — прогресс-бар загрузки, кнопка «Отмена»",
			"Режим цены пункта «%» — для крипты",
			"Крипто-счета BINAD, BYBITD",
			"Счёт «Личный» для EasyScalp",
			"Убрано окно терминала при запуске",
			"Выбор проп-счёта из списка",
			"Выбор секции рынка (акции, фьючерсы, валюта)",
			"Автоопределение префиксов стаканов",
			"Версионирование и автообновление через GitHub",
		},
	},
	{
		Version: "1.0",
		Date:    "2026-08-20",
		Changes: []string{
			"Первый релиз",
			"Загрузка объёмов с MOEX",
			"Запись в стаканы FSR и EasyScalp",
			"GUI с одной кнопкой «Загрузить»",
		},
	},
}

type VersionEntry struct {
	Version string
	Date    string
	Changes []string
}
