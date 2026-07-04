# sieve

Просеивает конфиги, пока что-то не сработает.

DPI не идёт на компромиссы — значит, и sieve не идёт. Портативный Windows TUI,
который тянет ассеты Flowseal zapret, прогоняет по очереди все вшитые
конфиги `winws` для Discord и YouTube, оставляет работающий и запоминает его
до следующего запуска.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/screenshot.png">
  <img alt="sieve TUI screenshot" src="assets/screenshot.png" width="1218" style="max-width: 100%; height: auto; aspect-ratio: 1218/714; border-radius: 8px; margin: 24px 0;">
</picture>

## Установка

```powershell
scoop bucket add elev1e1nSure https://github.com/elev1e1nSure/scoop-bucket
scoop install sieve
```

Либо взять `sieve-windows-amd64.exe` напрямую из
[релизов](https://github.com/elev1e1nSure/sieve/releases/latest).

## Требования

- Windows
- Права администратора во время работы
- Go 1.26+ и [`just`](https://github.com/casey/just) — только для сборки из исходников

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
просеивание или настройки, `Enter` подтверждает выбор:

```powershell
.\sieve.exe
```

В настройках доступны все сохраняемые параметры и обслуживающие действия:
обновление sieve и IPSet, остановка активного экземпляра, диагностика, сброс
результатов и очистка кэша Discord. После сохранения sieve возвращается в
главное меню.

Флаги остаются доступны для автоматизации. Запуск с любым флагом обходит
стартовое меню: параметры сохраняются или выполняется выбранное обслуживающее
действие, после чего программа завершается.

Одновременно может работать только один экземпляр sieve.

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

Запустить обслуживающие проверки:

```powershell
.\sieve.exe --diagnostics
.\sieve.exe --diagnostics --fix
.\sieve.exe --clear-discord-cache
```

Принудительно завершить активный экземпляр sieve, его `winws.exe` и удалить
оставшуюся службу WinDivert:

```powershell
.\sieve.exe --stop
```

`--stop` сначала просит активный TUI аккуратно завершить cleanup и только при
зависании принудительно завершает его дерево процессов. Активное окно сообщает,
почему оно было остановлено; ошибки WinDivert выводятся перед выходом.

Команда не завершает сторонние экземпляры `winws.exe`: старые процессы sieve
определяются по точному пути `%APPDATA%\sieve\bin\winws.exe`.

Показать метаданные сборки:

```powershell
.\sieve.exe --version
```

Обновить sieve до последнего релиза на GitHub:

```powershell
.\sieve.exe --update
```

В релизе должен быть подходящий бинарник — `sieve-windows-amd64.exe`
(подойдёт и старое имя `sieve.exe`). Публичные релизы работают без
дополнительной настройки. Для тестирования приватных релизов задайте
`GH_TOKEN` или `GITHUB_TOKEN` перед запуском `--update`. Если при обычном
запуске без флагов находится обновление, sieve тихо заменяет себя и
перезапускается. Замена выполняется скрытым helper-процессом с ограниченными
повторами и проверкой установленного файла. При ошибке sieve не запускает
старую версию повторно, а показывает причину при следующем запуске.

В меню и настройках используются `↑`/`↓`, `Enter`, `Esc` и `q`. Во время
просеивания выход — `q` или `Ctrl+C`. sieve завершает дерево `winws.exe`, дожидается
удаления службы WinDivert и сообщает об ошибке, если очистка не завершилась.
Процессы связаны Windows Job Object, поэтому `winws.exe` также завершается при
аварийном закрытии sieve.

## Команды для разработки

Список команд:

```powershell
just
```

Форматирование:

```powershell
just fmt
```

Тесты:

```powershell
just test
```

Локальная сборка:

```powershell
just build
```

Релизная сборка:

```powershell
just release-build
```

Очистить результаты сборки:

```powershell
just clean
```

## Данные во время работы

sieve хранит скачанные ассеты Flowseal и кэш здесь:

```text
%APPDATA%\sieve
```

Сохранённые настройки CLI лежат там же, в `settings.json`.
