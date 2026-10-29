# modernc.org/sqlite

Изучено 2026-10-29 по исходникам v1.61.0 (релиз 2026-10-21, внутри SQLite 3.54.1) в кэше модулей:
`doc.go`, `driver.go` (описание DSN), `sqlite.go` (`applyQueryParams`), `conn.go`, `stmt.go`, `tx.go`.
SQLite, переведённый с C на Go: работает без CGO, поэтому бинарник собирается статически
(`CGO_ENABLED=0`). Поддерживает windows/amd64, windows/386, linux/amd64, linux/arm64 и другие.

## Подключение

```go
import _ "modernc.org/sqlite"   // регистрирует драйвер "sqlite"

db, err := sql.Open("sqlite", path+"?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=1&_txlock=immediate")
```

- DSN — обычный путь или `file:`-URI, после `?` параметры. У обычного пути драйвер отрезает
  всё после первого `?`; собственные параметры SQLite (`mode=ro` и т. п.) работают только с `file:`.
- Параметры драйвера (применяются к каждому новому соединению пула):
  - `_busy_timeout` (`_timeout`) — сколько мс ждать блокировку вместо мгновенного `SQLITE_BUSY`;
  - `_journal_mode` (`_journal`) — `WAL`: читатели не блокируют писателя;
  - `_synchronous` (`_sync`) — `NORMAL` безопасен в WAL и быстрее `FULL`;
  - `_foreign_keys` (`_fk`) — включает проверку внешних ключей (в SQLite выключена по умолчанию);
  - `_txlock` — `immediate`: `BeginTx` выполняет `BEGIN IMMEDIATE` и сразу берёт блокировку записи.
    Read-only транзакции (`sql.TxOptions{ReadOnly: true}`) остаются обычным `BEGIN`;
  - `_pragma=name(value)` — произвольная PRAGMA без проверки, сокращённые ключи выше предпочтительнее;
  - `_time_format`, `_time_integer_format`, `_inttotime`, `_texttotime`, `_timezone` — работа с временем.
- Неизвестное значение сокращённого ключа — ошибка соединения, а не молчаливое игнорирование.

## Подводные камни

- **Время.** По умолчанию `time.Time` пишется строкой `t.String()` вместе с показаниями
  монотонных часов (`m=+0.001`); функции даты SQLite такую строку не понимают. Мы не передаём
  `time.Time` в драйвер: храним `INTEGER` unix-секунды UTC и конвертируем в коде.
- **Пул соединений.** `database/sql` по умолчанию открывает соединения без ограничения, у каждого
  свой кэш страниц. Ограничиваем `db.SetMaxOpenConns(4)`.
- **Состояние соединения.** PRAGMA, выполненная через `Exec`, остаётся на одном соединении пула,
  другие её не увидят. Настройки для всех соединений — только через DSN.
- **Блокировки в WAL.** Транзакция, начатая обычным `BEGIN`, которая сначала читает, а потом пишет,
  может получить `SQLITE_BUSY` без ожидания `busy_timeout`, если другой писатель успел изменить базу.
  `_txlock=immediate` это исключает. Одиночные операторы вне транзакции ждут `busy_timeout`.
- **Несколько операторов.** `Exec` выполняет SQL-скрипт из нескольких операторов, разделённых `;`
  (`stmt.go`, ветка «Multi-statement script»). Так применяем файлы миграций.
- **modernc.org/libc.** Версия libc в нашем `go.mod` должна совпадать с той, что в `go.mod`
  sqlite (сейчас v1.79.0). Обновляем sqlite командой `go get modernc.org/sqlite@vX`,
  libc отдельно не трогаем.
- **Скорость.** Переведённый код медленнее C-сборки примерно в 1,3–2 раза на тяжёлых запросах.
  Для бота неважно, но индексы для `WHERE` / `ORDER BY` / `GROUP BY` нужны.
- **Windows и тесты.** Открытый файл базы нельзя удалить: в тестах закрывать `Store` через
  `t.Cleanup`, зарегистрированный после `t.TempDir()`.
- **STRICT-таблицы.** SQLite 3.37+ проверяет типы колонок (`INTEGER`, `REAL`, `TEXT`, `BLOB`, `ANY`).
  Используем `STRICT` во всех таблицах.
- `RETURNING` поддерживается (SQLite 3.35+), но в нём нельзя ссылаться на `excluded`.

## Ссылки

- https://pkg.go.dev/modernc.org/sqlite
- https://gitlab.com/cznic/sqlite (основной репозиторий), https://github.com/modernc-org/sqlite (зеркало)
- https://www.sqlite.org/wal.html, https://www.sqlite.org/lang_transaction.html, https://www.sqlite.org/stricttables.html
