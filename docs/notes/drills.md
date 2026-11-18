# Каталог упражнений: источники

Конспект к `bot/content/drills.yaml` (шаг 8а, часть 3). В каталоге только то, что существует: карта мастерской
Steam, чью страницу проверили, или то, что есть в самой игре. Перед тем как добавить или изменить упражнение,
страницу проверяют заново и записывают сюда.

## Как проверяли (2026-11-16)

- Страницы мастерской — `https://steamcommunity.com/sharedfiles/filedetails/?id=<ID>`, через WebFetch: название,
  автор, игра, даты, подписчики, что тренирует.
- Список серии 5E — поиск мастерской CS2: `https://steamcommunity.com/workshop/browse/?appid=730&searchtext=5E_Prac`.
- Анонсы Valve — Steam News API (обычный GET, без ключа):
  `https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=730&count=6&enddate=<unix>&feeds=steam_community_announcements&maxlength=0`.
- Подводные камни:
  - на **каждой** странице мастерской в HTML есть скрытые шаблоны уведомлений: «This item has been removed from the
    community because it violates Steam Community & Content Guidelines. It is only visible to you.» и «This item is
    incompatible with Counter-Strike 2». Загрузчик страниц выдаёт их как текст. Это не статус предмета: они
    стоят и на Aim Botz (13,8 млн подписчиков, обновлена в ноябре), а «only visible to you» анонимному
    посетителю не показать. Статус видно по описанию, числу подписчиков и дате обновления;
  - Steam отвечает 429 после 3–4 запросов подряд: страницы проверяли по две, с паузами;
  - `counter-strike.net/news/updates` без JavaScript пустая, wiki Valve (developer.valvesoftware.com) отвечает 403;
  - curl на рабочем компьютере падает на TLS (код 35, schannel); проверку сертификатов не отключаем —
    страницы проверяли через WebFetch.
- Годы у «Updated 23 Oct» Steam не показывает: это текущий год, 2026.

## Карты мастерской

| ID | Название | Автор | Опубликована | Обновлена | Подписчиков | Что тренирует |
|---|---|---|---|---|---|---|
| 3070244462 | Aim Botz - Aim Training (CS2) | Mr. uLLeticaL™-S | 02.11.2023 | 6 ноя | 13 826 745 | Боты вокруг игрока: стоят или двигаются, с бронёй или без, выглядывают из-за стен; бесконечные патроны, всё оружие |
| 3070715607 | Yprac Hub by Yesber | Yesber | 03.11.2023 | 15 окт | 1 115 766 | Режимы: Aim Trainer, Prefire (64+ маршрутов), Utility (1400+ раскидок дымов, флешек, молотовов, HE), Shortcuts, Sandbox (все официальные карты), Bot Arena. Для каких карт префаеры и раскидки — в описании не сказано |
| 3232466864 | 5E_Prac_Mirage_Prefire | 5EPlay (7ychu5, Ba1eno) | 26.04.2024 | 23 окт | 3 309 934 | Префаеры: 4 уровня сложности, 8 маршрутов захода и ретейка |
| 3244203953 | 5E_Prac_Dust2_Prefire | 5EPlay | 11.05.2024 | 23 окт | 3 417 115 | То же, Dust2 |
| 3251878584 | 5E_Prac_Inferno_Prefire | 5EPlay | 21.05.2024 | 23 окт | 1 586 702 | То же, Inferno (своего описания нет, только описание серии) |
| 3270706131 | 5E_Prac_Vertigo_Prefire | 5EPlay | 18.06.2024 | 23 окт | 669 784 | То же, Vertigo (только описание серии) |
| 3272008480 | 5E_Prac_Ancient_Prefire | 5EPlay | 20.06.2024 | 23 окт | 1 103 859 | Префаеры: 4 уровня, 8 маршрутов |
| 3275080617 | 5E_Prac_Anubis_Prefire | 5EPlay | 25.06.2024 | 23 окт | 971 952 | Префаеры: 4 уровня, 8 маршрутов |
| 3279281339 | 5E_Prac_Nuke_Prefire | 5EPlay | 30.06.2024 | 23 окт | 1 052 472 | Префаеры: 4 уровня, 8 маршрутов. **В комментариях: «Broken since last patch», «defence mode is not working»** — когда написаны, страница не говорит; пользователь проверит на контрольной точке 2 (решение 2026-11-18) |
| 3369993742 | 5E_Prac_Train | 5EPlay | 22.11.2024 | 23 окт | 433 641 | Префаеры: 4 уровня, 7 маршрутов; 4 набора раскидок гранат |
| 3532966737 | 5e_Prac_Overpass | 5EPlay | 23.07.2025 | 23 окт | 293 645 | Префаеры: 4 уровня, 9 маршрутов; раскидки гранат |
| 3716900703 | 5E_Prac_cache | 5EPlay | 29 апр (2026) | 23 окт | 237 139 | Префаеры: 4 уровня, 8 маршрутов; 4 набора раскидок гранат |

Как упражнения каталога их используют:

- `aim_botz_first_shot`, `aim_botz_counter_strafe` — Aim Botz: одиночные выстрелы с остановкой; шаг, контрстрейф, выстрел.
  Карта ничего не делает за игрока, упражнение — в тексте задания.
- `prefire_5e` — карта серии 5E для карты матча (`by_map`); после матча на карте без версии 5E упражнение не даётся.
- `yprac_prefire`, `yprac_utility`, `yprac_flashes` — режимы Prefire и Utility в Yprac Hub.

## Встроенное в игру

- **Ретейки** (`retakes`) — официальный режим CS2 с обновления 22.10.2025. Источник — анонсы Valve в Steam News:
  «Re-Retakes» (22.10.2025 23:00 UTC): «In this classic game mode, each round drops you in to a fast-paced 4v3
  after-plant scenario»; «Counter-Strike 2 Update» того же дня: «Adding Retakes as official game mode supporting
  Defusal Group Alpha and Defusal Group Delta maps … on official matchmaking servers».
- **Разбор демки** (`demo_review`) — демо-плеер CS2 и `demo_gototick`: тики журнала проверены в CS2 2026-11-12
  (`docs/DECISIONS.md`).

## Не вошли

- **Recoil Master - Spray Training (CS2)** (3100869952, обновлена 6 ноя, 4,4 млн подписчиков) — существует,
  но контроль спрея не мерит ни одно правило находок: давать его заданием не к чему.
- **Fast Aim / Reflex Training** — страницу не проверяли.
- Старые карты Yprac (Mirage Guide и др.) — по bo3.gg в CS2 не работают, вместо них Yprac Hub.
