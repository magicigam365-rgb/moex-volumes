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
		Date:    "2026-08-24",
		Changes: []string{
			"Добавлено версионирование и справка по программе",
			"Удалённое обновление через GitHub Releases",
			"Выбор источника данных: MOEX API (ограничения) vs проп-сервер (из папки)",
			"Выбор проп-счёта: выпадающий список подключённых пропов",
			"Убрано поле ввода префиксов — определяются автоматически",
		},
	},
	{
		Version: "1.0",
		Date:    "2026-08-20",
		Changes: []string{
			"Загрузка оборотов с MOEX ISS API (5 торговых дней)",
			"Расчёт крупного лота: Vol = ROUND(avgTurnover / (Price × LotSize) × 0.01)",
			"DOM/кластеры: FilledAt, BigAmount, HugeAmount",
			"Рабочие объёмы First..Fifth (OrderSize1..5)",
			"Запись в стаканы FSR (C-формат _Settings.tmp)",
			"Запись в EasyScalp: AppSettings.xml + DOM-шаблоны + Trade-файлы",
			"GUI (lxn/walk): одна кнопка «Загрузить», автосохранение настроек",
			"CLI-режим: -cli, -config, -dry-run",
			"Бэкапы файлов перед записью",
			"Автоопределение префиксов стаканов",
			"Опциональные FSR и EasyScalp — программа работает без них",
			"reference.json — опциональный справочник max_lots",
		},
	},
}

type VersionEntry struct {
	Version string
	Date    string
	Changes []string
}
