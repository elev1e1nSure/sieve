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

Флаги не запускают TUI. Они либо сохраняют настройки, либо выполняют одно
обслуживающее действие, печатают результат и завершаются.
Просеивание начинается только без флагов:

```powershell
.\sieve.exe
```

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
перезапускается.

При старте sieve добавляет папку со своим исполняемым файлом в `PATH`
текущего пользователя. Пропустить это поведение можно так:

```powershell
.\sieve.exe --no-add-path
```

Выход — `q` или `Ctrl+C`. sieve убивает `winws.exe`, подчищает за собой
службы WinDivert и тихо сообщает, что просеивание окончено.

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
