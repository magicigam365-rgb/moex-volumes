# MOEX Volumes

Автоматическая загрузка объёмов MOEX для настройки стаканов FSR Launcher и EasyScalp. Заменяет Excel-макрос MOEX_PB.xlsm.

**Платформа:** Windows (x64)  
**Язык:** Go + lxn/walk (GUI)

---

## Возможности

- Загрузка оборотов с MOEX ISS API (средние за N дней)
- Расчёт крупного лота (Vol), DOM/кластеров и рабочих объёмов
- Автоматическая запись в стаканы FSR и EasyScalp
- Встроенная справка/инструкция по эксплуатации
- Автообновление через GitHub Releases (с прогресс-баром)
- Поддержка крипто-счетов BINAD (CCUR), BYBITD (linear, spot)
- CLI-режим (`-cli`, `-config`, `-dry-run`)

## Установка

1. Скачайте `MOEX_Volumes.exe` из [Releases](https://github.com/magicigam365-rgb/moex-volumes/releases)
2. Положите рядом `config.json` (создаётся автоматически при первом запуске)
3. Запустите

## Быстрый старт

1. Запустите `MOEX_Volumes.exe`
2. Выберите счёт из списка (определяется автоматически)
3. Нажмите «Загрузить»
4. Готово — стаканы обновлены

> **Важно:** перед загрузкой закройте EasyScalp. FSR можно не закрывать.

## Настройки

| Параметр | По умолчанию | Описание |
|----------|-------------|----------|
| Торговых дней | 5 | Сколько дней брать для расчёта среднего оборота |
| Заполнение (k_load) | 0.5 | Множитель для FilledAt / BigAmount / HugeAmount |
| Объём 1 (k_vol1) | 1.0 | Множитель для крупных объёмов (BigAmount) |
| Объём 2 (k_vol2) | 2.0 | Множитель для очень крупных объёмов (HugeAmount) |
| Цена пункта | — | Стоимость пункта в валюте счёта |
| Рабочие объёмы x1..x4 | 1, 2, 3, 0.5 | Множители First..Fourth_WorkAmount |

### Режим цены пункта

- **₽/$** — фиксированная стоимость (1.0 = 1 рубль за пункт)
- **%** — процент от стоимости контракта (цена × лот). Удобно для крипты.

Настройки сохраняются автоматически в `config.json`.

## Выбор счёта

Единый выпадающий список определяет источник данных:

| Источник | Счета | Данные берутся из |
|----------|-------|-------------------|
| FSR | Lite Invest, Whitelist, TINKD, TRNSQD | `...\Data\MVS\{Prefix}.{Section}.{Ticker}_Settings.tmp` |
| EasyScalp | Ваш аккаунт, Личный | `...\Config\Settings\Trade_{GUID}_Settings.xml` |
| Крипто | BINAD, BYBITD | `...\Data\backup\MVS\...` |

## Секции рынка

**EasyScalp** — STOCK (акции), FUT (фьючерсы), CURRENCY (валюта)  
**FSR** — TQBR (акции), FUT (фьючерсы), MTQR (фьючерсы MT), CCUR (валюта)

## Расчёт

**Крупный лот (Vol):**
- Акции: `Vol = ROUND(avgTurnover / (Price x LotSize) x 0.01)`
- Фьючерсы: `Vol = ROUND(lots x 0.01)`

**DOM/кластеры** (из `_Settings.tmp`):
- FilledAt — порог заполнения
- BigAmount — крупный объём
- HugeAmount — очень крупный объём

**Рабочие объёмы** (из Trade XML / _Settings.tmp):
- First_WorkAmount .. Fourth_WorkAmount
- Fifth_WorkAmount = 1 лот

## Структура файлов

```
MOEX_Volumes.exe      — программа
config.json           — настройки (рядом с exe, создаётся автоматически)
reference.json        — ограничения по бумагам (опционально)
```

**FSR Launcher:**
```
...\Data\MVS\{Prefix}.{Section}.{Ticker}_Settings.tmp
```

**EasyScalp:**
```
...\Config\Settings\Trade_{GUID}_Settings.xml
...\Config\Settings2\AppSettings.xml
```

## CLI-режим

```bash
MOEX_Volumes.exe -cli                        # режим командной строки
MOEX_Volumes.exe -cli -config myconfig.json  # с указанием конфига
MOEX_Volumes.exe -cli -dry-run               # без записи файлов
```

## Обновление

При запуске программа проверяет GitHub на наличие новой версии. Если обновление найдено — диалог «Пропустить / Скачать» с прогресс-баром загрузки.

## Решение проблем

| Проблема | Решение |
|----------|---------|
| Рабочие объёмы = 0 | Проверьте цену пункта. Для крипты попробуйте режим % |
| Стаканы не обновились | Закройте EasyScalp и попробуйте снова |
| Программа не запускается | Положите `config.json` рядом с exe |
| Не видит пропы | Убедитесь что FSR/EasyScalp установлен и содержит `_Settings.tmp` файлы |

## VK

https://vk.me/join/in94ilwPA5PTdJy2LI6w5MQGCCoY3U/mA4g=
