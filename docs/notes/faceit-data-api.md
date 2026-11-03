# FACEIT Data API

Изучено 2026-11-03: документация docs.faceit.com (Data API, API Keys), спецификация OpenAPI
`https://open.faceit.com/data/v4/docs/swagger.json` (Swagger 2.0, 64 пути), клиент FACEIT в исходниках
cs-demo-manager и живые ответы для своего аккаунта, записанные утилитой `cmd/faceitrecord`.

## Основное

- База `https://open.faceit.com/data/v4`, только `GET`, ответы JSON.
- Ключ — server-side API key приложения в App Studio (developers.faceit.com), заголовок
  `Authorization: Bearer <key>`. Client-side ключи — для кода, который видит пользователь, нам не нужны.
- Ошибки по спецификации: 400, 401, 403, 404, 429, 503. Тело ошибки Data API:
  `{"errors":[{"code":"err_nf0","http_status":404,"message":"The resource was not found.","parameters":[]}]}`.
- **Неверный ключ — 400, а не 401**: `{"error":"invalid_token","error_description":"Token was not recognised"}`.
- **Лимит.** В документации чисел нет. В каждом ответе есть заголовки `RateLimit-Limit: 20, 20;w=1`,
  `RateLimit-Remaining`, `RateLimit-Reset` (секунды до сброса) и
  `X-Faceit-Ratelimit-Name: gateway-open-faceit-ratelimit`, то есть 20 запросов в секунду.
  Цифра «10 000 запросов в час» из сторонних проектов заголовками не подтверждается.

## Эндпоинты, которые используем

| Что | Запрос | Главное в ответе |
|---|---|---|
| Игрок по нику | `GET /players?nickname=<ник>` | `player_id`, `nickname`, `country`, `games.cs2.{skill_level, faceit_elo, game_player_id}`, `steam_id_64`, `faceit_url` |
| Игрок по ID | `GET /players/{player_id}` | то же |
| Поиск игроков | `GET /search/players?nickname=&game=cs2&limit=` | `items[].{player_id, nickname, country, games[].{name, skill_level}}` |
| История матчей | `GET /players/{player_id}/history?game=cs2&limit=1..100&offset=0..1000` | `items[].{match_id, status, started_at, finished_at, results.{winner, score}, teams.faction1/2.players[]}` |
| Детали матча | `GET /matches/{match_id}` | `status`, `best_of`, `results`, `detailed_results`, `teams.faction1/2.{faction_id, name, leader, roster[]}`, `voting.map`, `demo_url`, `instances` |
| Статистика матча | `GET /matches/{match_id}/stats` | `rounds[]` — по одному на карту: `round_stats`, `teams[].{team_id, team_stats, players[].player_stats}` |

Поля, которые читает наш клиент:

- игрок: `games.cs2.game_player_id` — SteamID64 (17 цифр), `skill_level` 1–10, `faceit_elo`;
  нет ключа `cs2` в `games` — аккаунт не играл в CS2;
- состав в деталях: `roster[].{player_id, nickname, game_player_id (SteamID64), game_skill_level}`;
- карта в деталях: `voting.map.pick[0]` (`de_mirage`), название — `voting.map.entities[].name`
  у элемента, где `game_map_id` / `class_name` / `guid` равны `pick`;
- статистика: `round_stats.{Map, Rounds, Score "16 / 19", Winner = team_id}`;
  `team_stats.{"Final Score", "First Half Score", "Second Half Score", "Overtime score", "Team Win" ("0"/"1"), Team}`;
  `player_stats`: `Kills`, `Deaths`, `Assists`, `Headshots`, `Headshots %`, `MVPs`, `ADR`, `Damage`, `K/D Ratio`,
  `K/R Ratio`, `Result`, `Double/Triple/Quadro/Penta Kills`, `First Kills`, `Entry Count`, `Entry Wins`,
  `1v1Count`, `1v1Wins`, `1v2Count`, `1v2Wins`, `Clutch Kills`, `Flash Count`, `Flash Successes`,
  `Enemies Flashed`, `Utility Damage`, `Utility Count`, `Sniper Kills`, `Pistol Kills`, `Knife Kills`, `Zeus Kills` и др.

## Подводные камни

- **Регистр ника.** `/players?nickname=` ищет с учётом регистра: `KOLYX` и `Kolyx` вместо `kolyx` дают 404.
  `/search/players` регистр не учитывает и ищет нечётко (по началу ника). Клиент при 404 спрашивает поиск
  и берёт игрока с тем же ником без учёта регистра.
- **`from` в истории.** В документации: «1 month ago if not specified». На деле без `from` ответ
  содержит `"from": 0` и матчи любой давности: с `to` = 45 дней назад и `limit=100` пришли матчи с июня.
  `from` не передаём.
- **В истории нет карты**, только счёт и победитель. Карта есть в деталях матча и в статистике.
  Для `/matches` — история плюс детали пяти матчей: 6 запросов.
- **`results.score` в CS2 — счёт по раундам** (16:19) и в истории, и в деталях, для матчмейкинга BO1.
  У матчей BO3 (хабы, турниры) там, вероятно, счёт по картам — не проверяли.
  `detailed_results[].factions.factionN.score` — раунды по каждой карте.
- **Регистр статуса:** в истории `finished`, в деталях `FINISHED`. В 100 последних матчах истории
  встретился только `finished`.
- **Статистика — строки**, в том числе числа: `"Kills": "21"`, `"ADR": "63.8"`. Клиент читает и строку,
  и число; неразбираемое число считается ошибкой: значит, FACEIT сменил формат.
- **`voting` без схемы** в спецификации. Клиент разбирает его отдельно, сбой в нём не ломает матч.
- `team_id` в статистике равен `faction_id` в деталях. У матчмейкинга это ID капитана (`leader`),
  а название команды — `team_<ник капитана>`.
- Название карты в статистике бывает вида `workshop/<id>/<карта>` (по исходникам cs-demo-manager).
- `faceit_url` содержит заглушку `{lang}`: `https://www.faceit.com/{lang}/players/<ник>`.
- `demo_url`: `https://demos-europe-central.backblaze.faceit-cdn.net/cs2/<match_id>-1-1.dem.zst`.
  Без Downloads API по нему не скачать. Подписанная ссылка из браузера (шаг 7) — с другого хоста.
- У старых матчей CS:GO вместо `roster` бывает `roster_v1` (по cs-demo-manager). В матчах CS2 не встречали.
- `skill_level` в поиске — строка, и была `"0"`: уровень брать только из профиля.
- `game_player_name` и `steam_nickname` — ники в Steam, могут отличаться от ника FACEIT и друг от друга.
- ID игрока — UUID, ID матча — `1-<UUID>`. Перед подстановкой в путь клиент проверяет формат.

## Наш клиент (`internal/faceit`)

- `New(apiKey, opts...)`; `WithBaseURL` для тестов, `WithLogger`, `WithHTTPClient`.
- Попытка — 10 с; до 3 попыток при сетевых ошибках, 429 и 5xx. Паузы 0,5 и 1 с или сколько просят
  `Retry-After` / `RateLimit-Reset` (не больше 5 с). 4xx не повторяем. Если пауза не помещается
  в дедлайн ctx, ошибка возвращается сразу.
- Ограничение частоты: token bucket на весь клиент, 5 запросов в секунду, запас 10.
- Ответ не больше 2 МиБ (самый большой, статистика матча, ~25 КБ).
- Ошибки — `*StatusError{Op, Status, Code}`, для 404 `errors.Is(err, faceit.ErrNotFound)`.
  В ошибках и логах только операция (`find player`, `get match`), статус и код FACEIT: URL с ником
  и ID не попадает никуда, `*url.Error` от транспорта разворачивается.
- `ParseNickname` / `ParseProfileLink`: ник, `@ник`, ссылки `faceit.com/<язык>/players/<ник>/...`
  с `www` и без, без схемы, `players-modal`. Ник — буквы, цифры, `-`, `_`, `.`, 2–32 символа:
  правила FACEIT не опубликованы, в поддержке сказано только «не меньше трёх символов».

## Фикстуры

- `internal/faceit/testdata/` — настоящие ответы, записанные из `bot/` командой
  `go run ./cmd/faceitrecord -nickname <ник> [-match <match_id>]...`. Утилита берёт ключ из `.env`,
  заменяет ники, ID игроков, команд и матчей и SteamID подставными (`player01`,
  `00000000-0000-4000-8000-000000000001`, `76561190000000001`) одинаково во всех файлах, убирает аватары
  и друзей и перед записью проверяет, что исходных значений не осталось.
- Остаются настоящими время матчей, счёт, статистика, ELO, уровни и страна профиля.
- Если FACEIT поменяет формат, перезаписать той же командой. Утилита печатает пробы: регистр ника,
  `from` в истории, заголовки лимита.

## Ссылки

- https://docs.faceit.com/docs/data-api/ и https://docs.faceit.com/docs/data-api/data
- https://open.faceit.com/data/v4/docs/swagger.json
- https://docs.faceit.com/getting-started/authentication/api-keys
- https://support.faceit.com/hc/en-us/articles/207554609 — правила ника
- https://github.com/akiver/cs-demo-manager/tree/main/src/node/faceit-web-api
