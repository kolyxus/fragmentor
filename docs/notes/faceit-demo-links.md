# Подписанные ссылки на демки FACEIT

Изучено 2026-11-05/06 (шаг 7): документация Downloads API FACEIT, пример ссылки в документации Leetify,
список хостов в правах расширения Faceit-Extender. Ответы хранилища проверены одним запросом
`curl -r 0-0` по протухшей ссылке kolyx от 28.10.2026. Сама ссылка нигде не сохранена.

## Что это

- FACEIT хранит демки в Backblaze B2 и отдаёт их только по **presigned URL S3 API** (подпись AWS SigV4).
  Браузер получает такую ссылку, когда на странице матча нажимают «Смотреть демо», и сразу по ней скачивает.
  Официально ссылку выдаёт `POST /download/v2/demos/download` Downloads API: на вход `resource_url`
  (это `demo_url` из Data API), на выход `payload.download_url`. Доступа к этому API у нас нет (шаг 14).
- Вид ссылки (значения заменены):

  ```
  https://demos-europe-central-faceit-cdn.s3.eu-central-003.backblazeb2.com/cs2/<match_id>-1-1.dem.zst
    ?X-Amz-Algorithm=AWS4-HMAC-SHA256
    &X-Amz-Credential=<key id>%2F<yyyymmdd>%2F%2Fs3%2Faws4_request
    &X-Amz-Date=<yyyymmdd>T<hhmmss>Z
    &X-Amz-Expires=299
    &X-Amz-SignedHeaders=host
    &x-id=GetObject
    &X-Amz-Signature=<64 hex>
  ```

  - хосты: `demos-europe-central-faceit-cdn.s3.eu-central-003.backblazeb2.com`, `demos-us-east-faceit-cdn.s3.us-east-005.backblazeb2.com`;
  - в `X-Amz-Credential` регион пустой (`…/20261028//s3/…`);
  - **`X-Amz-Expires=299`**: ссылка живёт 4 мин 59 с от `X-Amz-Date`. Срок записан в самой ссылке,
    поэтому протухшую ссылку видно без запроса.
- Подписан только заголовок `host`. Свои заголовки (`Range`, `Accept-Encoding`) добавлять можно.
- Ссылка — это пропуск к файлу на несколько минут: в логи, ошибки и тесты она не попадает, в базе живёт,
  пока задача не закончится.
- `demo_url` из Data API (`https://demos-europe-central.backblaze.faceit-cdn.net/cs2/<match_id>-1-1.dem.zst`)
  без подписи не скачивается. Такую ссылку бот узнаёт по хосту `demos-*.backblaze.faceit-cdn.net`
  и объясняет, где взять нужную.

## Ответы хранилища

| Запрос | Ответ |
|---|---|
| Протухшая подпись | **401**, `Content-Type: application/xml`, `<Code>UnauthorizedAccess</Code>`, `<Message>Request has expired given timestamp: '20261028T120719Z' and expiration: 299</Message>` |
| Тот же объект без подписи | **401**, `UnauthorizedAccess`, `bucket is not authorized: demos-europe-central-faceit-cdn` |

- Это не 403, как у AWS S3 (`AccessDenied`, «Request has expired»). Поэтому 400, 401 и 403 бот считает одним:
  «ссылка не работает — пришли свежую». 404 — демки в хранилище нет.
- По S3 срок подписи проверяется при получении запроса, и начатое скачивание продолжается, даже если
  ссылка истекла посреди него. Поэтому скачивание должно *начаться* до срока. У B2 это не проверено:
  нужна свежая ссылка (живая проверка шага 7).
- В Windows `curl` (schannel) падает на `CRYPT_E_NO_REVOCATION_CHECK`, поэтому для ручных проверок нужен
  `--ssl-no-revoke`. Go проверяет сертификат сам, без этой проблемы.

## Как проверяет бот (`faceit.ParseDemoLink`)

- только `https`, без логина и порта;
- хост — `^demos-[a-z0-9-]+-faceit-cdn\.s3\.[a-z0-9-]+\.backblazeb2\.com$` без учёта регистра;
- путь без экранирования — `/cs2/1-<UUID в нижнем регистре>-1-1.dem.zst`, то есть матч CS2 на одну карту;
- есть непустой `X-Amz-Signature`, иначе `ErrUnsignedLink`;
- `Expires` = `X-Amz-Date` + `X-Amz-Expires`, `Lifetime` = `X-Amz-Expires`; без них срок неизвестен (нули);
- хост и query передаются как пришли: их покрывает подпись. Фрагмент `#…` отбрасывается.

В сообщении бот ищет слово с `faceit-cdn`, `backblaze` или `.dem` и снимает с него кавычки, скобки и знаки
препинания вокруг (`internal/telegram/demo.go`, `demoLinkIn`).

## Как пользователь достаёт ссылку

1. На странице своего матча на faceit.com нажать «Смотреть демо» (Watch demo). Браузер начнёт скачивать
   `.dem.zst`, загрузку можно сразу отменить.
2. Открыть загрузки: Ctrl+J (`chrome://downloads`).
3. Правой кнопкой по ссылке под именем файла → «Копировать адрес ссылки».
4. Прислать боту сразу: ссылка живёт 5 минут.

Этот текст бот показывает по кнопке «Как достать ссылку». Названия пунктов меню браузера нужно сверить
вживую: Chrome, Яндекс Браузер, Firefox.

## Ссылки

- https://docs.faceit.com/getting-started/Guides/download-api — Downloads API
- https://leetify.com/blog/faceit-demo-upload-api/ — пример подписанной ссылки FACEIT
- https://addons.mozilla.org/en-US/firefox/addon/faceit-extender/ — права расширения с хостом `demos-us-east-faceit-cdn.s3.us-east-005.backblazeb2.com`
- https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html — presigned URL в S3
