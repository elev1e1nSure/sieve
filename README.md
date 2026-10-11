# sieve

[![Release](https://img.shields.io/github/v/release/elev1e1nSure/sieve)](https://github.com/elev1e1nSure/sieve/releases/latest)
[![CI](https://github.com/elev1e1nSure/sieve/actions/workflows/ci.yml/badge.svg)](https://github.com/elev1e1nSure/sieve/actions/workflows/ci.yml)

**Просеивает конфиги, пока что-то не сработает.**

DPI не идёт на компромиссы — значит, и sieve не идёт. Портативный Windows TUI,
который тянет свежий релиз Flowseal zapret, прогоняет по очереди все его
стратегии `winws` для Discord и YouTube, оставляет работающую и запоминает её
до следующего запуска.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot.png">
  <img alt="sieve TUI screenshot" src="assets/screenshot.png" width="1218" style="max-width: 100%; height: auto; aspect-ratio: 1218/714; border-radius: 8px; margin: 24px 0;">
</picture>

## Установка

Требования: Windows, права администратора во время работы.

```powershell
scoop bucket add elev1e1nSure https://github.com/elev1e1nSure/scoop-bucket
scoop install sieve
```

Либо взять `sieve-windows-amd64.exe` напрямую из
[релизов](https://github.com/elev1e1nSure/sieve/releases/latest).

Сборка из исходников требует Go 1.26+ и [`just`](https://github.com/casey/just).

## Использование

Собрать и запустить:

```powershell
just build
.\sieve.exe
```

Запустить из исходников:

```powershell
just run
```

Указать свой таймаут проверки соединения:

```powershell
just run-timeout 10
.\sieve.exe --test-timeout 10
```

При запуске без флагов sieve открывает стартовое меню. Стрелками можно выбрать
просеивание или настройки, `Enter` подтверждает выбор. Для запуска подбора и
активации обхода напрямую без меню используется подкоманда `run`:

```powershell
.\sieve.exe run
```

В настройках доступны все сохраняемые параметры и обслуживающие действия:
обновление sieve и IPSet, остановка активного экземпляра, диагностика, сброс
результатов и очистка кэша Discord. После сохранения sieve возвращается в
главное меню.

Флаги обходят стартовое меню: параметры сохраняются или выполняется выбранное
обслуживающее действие, после чего программа завершается. Одновременно может
работать только один экземпляр sieve.

**Управление:** в меню — `↑`/`↓`, `Enter` и `Esc`. Во время просеивания
выход — `Ctrl+C`, `Ctrl+O` переключает сырой лог `winws`, `T` сворачивает в
трей. sieve завершает дерево `winws.exe` и сообщает об ошибке, если остановка
не удалась. Службу WinDivert sieve не трогает: драйвер общий, им могут
пользоваться другие программы. Процессы связаны Windows Job Object, поэтому
`winws.exe` также завершается при аварийном закрытии sieve.

**Как выбирается стратегия.** Стратегии берутся из `general*.bat` скачанного
релиза Flowseal (если их нет — из встроенного снимка), стратегии со
ссылками на отсутствующие файлы пропускаются. Перед стартом sieve включает
TCP timestamps — без них не работают стратегии с `--dpi-desync-fooling=ts`.
Стратегия считается рабочей, если `discord.com` и `www.youtube.com` отвечают
минимум в двух попытках из трёх. Если не прошла ни одна, стратегии, которые
уже работали раньше, проверяются ещё раз. Через VPN проверка бессмысленна:
sieve предупредит, если интернет-трафик идёт через туннель.

**Команды для разработки:**

| Команда | Назначение |
|---|---|
| `just` | Список всех команд |
| `just build` | Локальная сборка |
| `just release-build` | Релизная сборка |
| `just run` | Запуск из исходников |
| `just test` | Тесты |
| `just sync-strategies` | Обновить встроенный снимок стратегий из релиза Flowseal |
| `just fmt` | Форматирование |
| `just lint` | Проверка линтером |
| `just check` | fmt + lint + test + build |
| `just clean` | Очистка результатов сборки |

## Конфигурация

Флаги сохраняются в `settings.json` и применяются при следующих запусках.

Сбросить кэш результатов конфигов перед запуском:

```powershell
.\sieve.exe --reset-cache
```

Отключить кэш конфигов для текущего запуска:

```powershell
.\sieve.exe --no-cache
```

Настроить списки Flowseal перед запуском:

```powershell
.\sieve.exe --update-ipset --ipset loaded
.\sieve.exe --ipset none
.\sieve.exe --ipset any
.\sieve.exe --domain discord.media --domain-file .\domains.txt
```

Включить фильтры игрового трафика:

```powershell
.\sieve.exe --game all
.\sieve.exe --game tcp
.\sieve.exe --game udp
```

Показать метаданные сборки:

```powershell
.\sieve.exe --version
```

## Обслуживание

Запустить диагностику системы:

```powershell
.\sieve.exe --diagnostics
.\sieve.exe --diagnostics --fix
```

Очистить кэш Discord:

```powershell
.\sieve.exe --clear-discord-cache
```

Обновить sieve до последнего релиза на GitHub:

```powershell
.\sieve.exe --update
```

Для приватных релизов задайте `GH_TOKEN` или `GITHUB_TOKEN` перед запуском.
При обычном запуске без флагов sieve тихо заменяет себя и перезапускается.
Замена выполняется скрытым helper-процессом с проверкой установленного файла.

Принудительно завершить активный экземпляр sieve и его `winws.exe`:

```powershell
.\sieve.exe --stop
```

`--stop` сначала просит активный TUI аккуратно завершить cleanup и только при
зависании принудительно завершает его дерево процессов. Команда не завершает
сторонние экземпляры `winws.exe` — старые процессы sieve определяются по
точному пути `%APPDATA%\sieve\bin\winws.exe`.

## Архитектура

sieve — Windows TUI на Bubble Tea и Lip Gloss. Скачивает релиз
[zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube),
разбирает команды `winws` из его `general*.bat`, прогоняет их по очереди,
проверяет доступность Discord и YouTube через HTTP и оставляет первую
работающую стратегию. Ассеты и стратегии лежат в `%APPDATA%\sieve`,
результаты кэшируются в `cache.json`, настройки — в `settings.json` там же.

Детальное описание архитектуры — в [CLAUDE.md](CLAUDE.md).

## Лицензия

Не указана.
