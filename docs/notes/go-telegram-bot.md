# github.com/go-telegram/bot

Изучено 2026-10-29 по исходникам v1.27.0 (релиз 2026-09-11, Bot API 10.3) в кэше модулей:
`bot.go`, `options.go`, `get_updates.go`, `wait_updates.go`, `process_update.go`,
`handlers.go`, `raw_request.go`, `build_request_form.go`, `errors.go`, README и `examples/`.
Библиотека живая (релизы каждые несколько недель), без внешних зависимостей, лицензия MIT.

## Основное

```go
b, err := bot.New(token, opts...)           // по умолчанию сразу вызывает getMe с таймаутом 5 с
b.RegisterHandler(bot.HandlerTypeMessageText, "start", bot.MatchTypeCommandStartOnly, h, mw...)
b.Start(ctx)                                // long polling, блокирует до ctx.Done()
b.SendMessage(ctx, &bot.SendMessageParams{ChatID: id, Text: "..."})
```

- Обработчик: `func(ctx context.Context, b *bot.Bot, update *models.Update)`, ошибку не возвращает.
- Все методы Bot API — методы `*bot.Bot` с сигнатурой `(ctx, *XxxParams) (result, error)`.
  Параметры передаются указателем, поля `omitempty` с нулевым значением не отправляются.
- Запрос — `POST {serverURL}/bot{token}/{method}` с телом `multipart/form-data`;
  сложные поля (массивы, структуры) кладутся в форму как JSON. Метод без полей уходит без тела.
- `models.Update`: `Message`, `EditedMessage`, `ChannelPost`, `CallbackQuery` и т. д. — указатели,
  проверять на nil. `Message.From` — `*models.User` (может быть nil в каналах), `Message.Chat` — значение.
- `models.User.LanguageCode` — IETF-тег «языка пользователя» (`ru`, `en`, `pt-br`), может быть пустым.
  Это **не обязательно язык интерфейса Telegram**. Проверено 2026-10-29 на Telegram Desktop
  (Windows):
  - система ru, интерфейс Telegram en → приходит `ru`, в том числе после полного перезапуска клиента;
  - система en, интерфейс Telegram ru → `ru`;
  - система en, интерфейс Telegram en → `en`, но не сразу: первые сообщения после смены ещё шли с `ru`.

  Описание бота клиент показывает сам на языке интерфейса, так что описание и ответы могут
  разойтись по языку. Как именно Telegram вычисляет код, в документации не сказано.

## Сопоставление обработчиков

- `RegisterHandler(type, pattern, matchType, f, middlewares...)`, типы: `HandlerTypeMessageText`,
  `HandlerTypeCallbackQueryData`, `HandlerTypeCallbackQueryGameShortName`, `HandlerTypePhotoCaption`.
- `MatchTypeExact`, `MatchTypePrefix`, `MatchTypeContains` сравнивают весь текст.
- `MatchTypeCommand` и `MatchTypeCommandStartOnly` ищут сущность `bot_command`; паттерн — имя без `/`.
  `CommandStartOnly` — команда в начале сообщения, поэтому `/start abc` (диплинк) тоже совпадает.
- `/start@fragmentor_ai_bot` с этими типами **не** совпадает: сравнивается весь текст сущности.
  В личных чатах клиенты так не пишут, бот отвечает на это подсказкой.
- Срез `text[offset+1:offset+length]` берётся по байтам, а смещения Telegram — в UTF-16.
  Для команды с нулевым смещением это одно и то же, для `MatchTypeCommand` после кириллицы — нет.
- Первый совпавший обработчик выигрывает; если не совпал ни один, вызывается `WithDefaultHandler`.
- Есть `RegisterHandlerRegexp` и `RegisterHandlerMatchFunc` для своих условий.

## Конкурентность и остановка

- `Start` запускает горутину `getUpdates` и `WithWorkers(n)` воркеров (по умолчанию 1),
  которые читают канал апдейтов (`WithUpdatesChannelCap`, по умолчанию 1024).
- **По умолчанию** воркер запускает обработчик через `go`, и `Start` при отмене ctx эти горутины
  **не ждёт**. С `WithNotAsyncHandlers()` обработчик выполняется внутри воркера, и `Start`
  возвращается только после завершения начатых обработчиков. Мы используем
  `WithNotAsyncHandlers()` + `WithWorkers(4)`.
- Обработчик получает ctx от `Start`: при остановке он сразу отменяется. Чтобы начатые
  обработчики доработали, наш middleware заменяет его на `context.WithoutCancel` + свой таймаут.
- Паники в обработчиках библиотека не ловит — нужен свой recover (у нас middleware).
- Middleware: `func(next bot.HandlerFunc) bot.HandlerFunc`, глобальные — `WithMiddlewares`,
  применяются ко всем апдейтам, включая обработчик по умолчанию. Первый в списке — внешний.
- Смещение апдейтов хранится только в памяти и подтверждается следующим `getUpdates`.
  Апдейты, уже полученные, но не обработанные к остановке, теряются (практически пустой буфер).
- `ProcessUpdate(ctx, upd)` — обработать апдейт вручную; с `WithNotAsyncHandlers` синхронно,
  этим пользуются наши тесты.

## Таймауты и сеть

- По умолчанию `pollTimeout` = 1 мин и `http.Client{Timeout: 1 мин}`; `getUpdates` просит
  Telegram держать запрос `pollTimeout - 1s`. Запас всего 1 с, поэтому мы задаём
  `WithHTTPClient(50*time.Second, &http.Client{Timeout: 60*time.Second})`.
- Таймаут клиента действует на все методы, поверх него — ctx вызова.
- `http.Client` без своего `Transport` берёт `http.DefaultTransport`, а он учитывает
  `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY`.
- После ошибки `getUpdates` библиотека ждёт 100 мс, удваивая до 5 с, при 429 — `retry_after`.
- `WithSkipGetMe()` отключает `getMe` в `New`; мы вызываем `GetMe` сами с ctx, который
  отменяется по сигналу. `WithCheckInitTimeout` меняет таймаут встроенного `getMe`.

## Ошибки

- Ответ `ok:false` превращается в `bot.ErrorForbidden` (403, например, пользователь заблокировал бота),
  `ErrorBadRequest`, `ErrorUnauthorized`, `ErrorNotFound`, `ErrorConflict` (409, второй процесс
  с тем же токеном), `*TooManyRequestsError` с `RetryAfter`, `*MigrateError`. Проверка — `errors.Is` / `errors.As`.
- Ошибки фонового `getUpdates` уходят в `WithErrorsHandler` (по умолчанию `log.Printf`).

## Безопасность

- Токен стоит в URL. В `*url.Error` от `http.Client.Do` библиотека заменяет токен на `***`
  (`raw_request.go`), так что сетевые ошибки безопасно логировать. Закреплено нашим тестом
  `TestErrorsDoNotLeakToken`: при обновлении библиотеки он поймает регрессию.
- `bot.Token()` и `FileDownloadLink()` возвращают строки с токеном — не логировать.
- Стандартный обработчик по умолчанию пишет в лог апдейт целиком (`%+v`): ники, тексты.
  Обязательно задавать свой `WithDefaultHandler`.
- `WithDebug()` пишет в лог тела запросов и ответов (тексты пользователей) — в проде не включать.

## Профиль бота

- `SetMyCommands` / `GetMyCommands`, `SetMyDescription` / `GetMyDescription`,
  `SetMyShortDescription` / `GetMyShortDescription` с полем `LanguageCode`.
  Пустой код — значение по умолчанию для всех языков без своего набора.
- Лимиты Bot API (в символах UTF-16): описание команды 1–256, имя команды 1–32
  (строчные латинские буквы, цифры, `_`), description до 512, short description до 120.
- `Get*` для языка без своего значения возвращает пустое значение, а не значение по умолчанию.
- Мы сначала читаем текущие значения и меняем только отличающиеся: при частых перезапусках
  лишние `setMy*` могут упереться во flood-лимит.

## Тесты

- `bot.WithServerURL(httptestServer.URL)` + `bot.WithSkipGetMe()` — бот ходит в фейковый сервер.
- Фейк разбирает `multipart/form-data` (`r.ParseMultipartForm`) и отвечает
  `{"ok":true,"result":...}`. Путь — `/bot<token>/<method>`.
- Сущность команды в тестовом апдейте задаём сами:
  `models.MessageEntity{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len("/start")}`.

## Ссылки

- https://github.com/go-telegram/bot
- https://pkg.go.dev/github.com/go-telegram/bot
- https://core.telegram.org/bots/api
