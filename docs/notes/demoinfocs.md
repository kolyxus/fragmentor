# demoinfocs-golang

Изучено 2026-11-03: исходники `v6.0.0-rc.1` в кэше модулей, README, папка `examples` на теге
(в архив модуля она не входит, это отдельный модуль), релизы на GitHub, pkg.go.dev. Всё ниже проверено
на двух демках FACEIT из `bot/testdata/` (de_ancient, 19 и 28 раундов, ноябрь 2026).

Наш код с библиотекой — только `bot/internal/demo` (решение в DECISIONS). Остальные пакеты видят
`demo.Match` и не импортируют demoinfocs.

## Версия

- `github.com/markus-wa/demoinfocs-golang/v6`, закреплена `v6.0.0-rc.1` (24.10.2026). Стабильной v6 нет,
  последняя стабильная — `v5.2.0` (21.04.2026), запасной вариант. Нужен Go 1.27.
- Что изменилось в v6 против v5 (заметки к `v6.0.0-rc.0`):
  - `NewParser(r, opts...)` с опциями из пакета `option`;
  - `ParseToEnd(ctx)`, а `Cancel()` убран — отмена через `ctx`;
  - сбои библиотеки возвращаются ошибками, а не паникой;
  - `Player.Name` и `SteamID64` берутся из контроллера игрока;
  - поменялись значения здоровья в `PlayerHurt`.
- Перейти на `v6.0.0`, когда она выйдет: `go get …/v6@v6.0.0`, затем `go run ./cmd/parsedemo -faceit testdata`.

## Запуск

```go
p := demoinfocs.NewParser(r, option.MsgQueueBufferSize(1000)) // r — уже распакованный поток
defer p.Close() // закроет и r, если r — io.Closer
p.RegisterNetMessageHandler(func(m *msg.CDemoFileHeader) { mapName = m.GetMapName() })
p.RegisterEventHandler(func(e events.Kill) { /* ... */ })
err := p.ParseToEnd(ctx) // ctx ограничивает время
```

- `NewParser` ничего не читает, чтение начинается в `ParseToEnd`. Возобновить разбор после возврата нельзя.
- Две горутины: одна читает демку, другая разбирает сообщения и вызывает обработчики. События рассылаются
  синхронно, обработчики работают по одному. Поля, которые заполнили обработчики, можно читать после возврата
  `ParseToEnd`: он ждёт, пока очередь опустеет.
- `MsgQueueBufferSize`: по умолчанию очередь равна числу тиков, от 50 000 до 500 000. На демке FACEIT:

  | Очередь | Время | Пик памяти |
  |---|---|---|
  | по умолчанию | 5,6 с | 103 МиБ |
  | 1000 | 5,7 с | 60 МиБ |
  | 0 (последовательно) | 15,2 с | 60 МиБ |

  Берём 1000.
- Другие опции: `IgnorePacketEntitiesPanic` — нужна для некоторых POV-демок;
  `UserCmdParsing` — по умолчанию разбираются только кнопки; `EagerVelocityTracking`;
  `DisableMimicSource1Events` — не включать, иначе пропадут `RoundStart`/`RoundEnd`, см. ниже.

## Ошибки и паники

- `ErrUnexpectedEndOfDemo` (совпадает и с `io.ErrUnexpectedEOF`) — демка обрезана. События до обрыва
  разосланы. Пустой поток даёт ту же ошибку.
- `ErrInvalidFileType` — первые 8 байт не `PBDEMS2`; у демок CS:GO там `HL2DEMO`.
- `ErrDecodeFailed` / `*DecodeError{Frame, Cause, Stack}` — битые данные или новая схема после обновления игры.
- Ошибка нашего `io.Reader` возвращается обёрнутой (`reading demo: %w`), `errors.Is` работает.
  Только «голые» `io.EOF` и `io.ErrUnexpectedEOF` библиотека считает обрывом демки.
- Отмена `ctx` возвращает `context.Cause(ctx)`. Уже отменённый `ctx` возвращается сразу.
- Паника в нашем обработчике: `ParseToEnd` сам паникует на вызывающей горутине с `*demoinfocs.PanicError{Value, Stack}`.
  Ловим `recover` (`demo.catch`). Останавливать разбор из обработчика — отменой `ctx`, не паникой.
- Многие геттеры (`Player.Health()`, `TeamState.ClanName()`, …) внутри зовут `PropertyValueMust` и паникуют,
  если свойства нет в схеме. Внутри обработчика это превращается в `PanicError`.
- Защита от подделанных демок: кадр не больше 16 МиБ (`DEM_AnimationData` — 64 МиБ), snappy расширяется
  не больше чем в 24 раза. Битый `DEM_FullPacket` пропускается с `ParserWarn{Type: WarnTypeBrokenFullPacket}`.

## Тики, время, карта

- `p.TickRate()` — тиков в секунду (64 на FACEIT), `p.TickTime()` — длина тика. Берутся из `CSVCMsg_ServerInfo`,
  о готовности сообщает событие `TickRateInfoAvailable`. До этого значения берутся из заголовка или равны `-1`.
- `gs.IngameTick()` — тик сервера, по нему считаем время: секунды = тики / `TickRate()`. В начале демки бывает
  странным, даже отрицательным.
- `p.CurrentFrame()` — кадр демки, не тик. `p.FrameCount()` у CS2 до конца разбора `-1`, а `Progress()` — 0.
- Карта: `msg.CDemoFileHeader.GetMapName()` (или `CSVCMsg_ServerInfo.GetMapName()`). Заголовок в v6 не экспортирован.

## Состояние игры

`gs := p.GameState()` — живое состояние на текущий момент разбора.

- `IsMatchStarted()`, `IsWarmupPeriod()`, `TotalRoundsPlayed()`, `OvertimeCount()`, `GamePhase()`:
  1 — предматч, 2 — первая половина (и первая половина овертайма), 4 — перерыв, 3 — вторая половина
  после смены сторон, 5 — конец игры.
- `gs.Team(common.TeamTerrorists)` / `TeamCounterTerrorists()` — `*TeamState` **стороны**, а не команды:
  `Score()` и `ClanName()` при смене сторон переходят на другую сторону.
- `gs.Participants()`:
  - `All()` — все, кого видели, включая вышедших, по SteamID64; боты с 0 идут первыми;
  - `Playing()` — подключённые на T или CT;
  - `FindByPawnHandle(h)`.
- `gs.Rules().ConVars()["mp_maxrounds"]` — `"24"` на FACEIT.
- `common.Player`:
  - `SteamID64` (у ботов 0), `Name` — ник из контроллера, на FACEIT это ник FACEIT, `Team` — сторона сейчас;
  - `IsBot`, `IsConnected`, `Health()`, `Armor()`;
  - `Kills()`, `Deaths()`, `Assists()`, `TotalDamage()` — табло игры, с FACEIT не совпадает (см. ниже).
- Свойства сущностей обновляются после игровых событий тика: `Health()` в обработчике `PlayerHurt` — здоровье
  на начало тика, до этого и следующих попаданий в том же тике.

## События

| Событие | Когда и что внутри | Подводные камни |
|---|---|---|
| `MatchStart` | `begin_new_match`: матч начался заново | На FACEIT приходит после разминки; сбрасываем всё посчитанное |
| `IsWarmupPeriodChanged` | начало и конец разминки | |
| `MatchStartedChanged` | `m_bHasMatchStarted` | В конце матча становится `false` сразу после последнего `RoundEnd` того же тика |
| `RoundStart`, `RoundEnd`, `RoundEndOfficial` | библиотека воссоздаёт их по свойствам игры (`m_eRoundWinReason`), игровых `round_start`/`round_end` в демках FACEIT нет | `RoundEnd.Winner` — сторона; при ничьей и рестарте (`Reason` = `Draw` / `GameStart`) победитель — `TeamSpectators`. Документация говорит, что счёт в `TeamState` к `RoundEnd` ещё не обновлён, но в демках FACEIT `ScoreUpdated` приходит раньше в том же тике. Не полагаемся, считаем сами. `RoundStart` первого раунда — в тике `MatchStart`, после него; у последнего раунда нет `RoundEndOfficial` |
| `RoundFreezetimeEnd` | `round_freeze_end`: конец заморозки, раунд пошёл | Ровно один на раунд во всех пяти демках. В этот момент `Participants().Playing()` с `IsAlive()` — кто начал раунд, 5 + 5 вместе с ботами |
| `ScoreUpdated`, `TeamSideSwitch`, `GameHalfEnded`, `OvertimeNumberChanged` | | `TeamSideSwitch` — в начале второй половины и второй половины овертайма; в начале овертайма стороны не меняются |
| `Kill` | `player_death`: `Killer`, `Victim`, `Assister`, `IsHeadshot`, `AssistedFlash`, `Weapon`, `PenetratedObjects`, `NoScope`, `ThroughSmoke`, `AttackerBlind`, `Distance` | Убийца `nil` — мир (бомба `planted_c4`, падение). В битых демках `nil` бывают и жертва, и убийца. Игроки определяются по `userid` (слот, `& 0xff`), а по pawn — только если слот не нашёлся (см. «Захват бота») |
| `PlayerHurt` | `player_hurt`: `Player` (жертва), `Attacker`, `Health` (после попадания), `HealthDamage` (сырой `dmg_health`, бывает на 1 меньше снятого), `HealthDamageTaken` (снятое здоровье — совпадает с уроном FACEIT), `ArmorDamage*`, `HitGroup`, `Weapon` | `HealthDamageTaken` считается от `Health()` той жертвы, которую нашла библиотека. `HitGroupNeck` (8) получает урон как грудь, не как голова: AK-47 — медиана 30 в шею, 28 в грудь, 123 в голову |
| `GenericGameEvent` | любое игровое событие с исходными полями `Data map[string]*msg.CMsgSource1LegacyGameEventKeyT` | Рассылается сразу **после** своего события (`Kill`, `PlayerHurt`, …) в том же тике |
| `WeaponFire` | выстрел; ~5000 за матч | |
| `PlayerFlashed` | `player_blind` + длительность | |
| `BulletDamage` | только CS2 после 22.07.2024 и только в записях MM/POV/full-tick | **В демках FACEIT нет** — ни `bullet_damage`, ни `bullet_impact` |
| `BotTakenOver` | `bot_takeover` | **В демках FACEIT нет**, хотя захват бота был |
| `ParserWarn` | некритичные проблемы, `Type` — `WarnType*` | |

Поля `player_death`: `userid`, `userid_pawn`, `attacker`, `attacker_pawn`, `assister`, `assister_pawn`, `assistedflash`,
`attackerblind`, `attackerinair`, `headshot`, `noscope`, `penetrated`, `thrusmoke`, `distance`,
`dmg_health`, `dmg_armor`, `hitgroup`, `weapon`, `weapon_itemid`, `weapon_fauxitemid`,
`weapon_originalowner_xuid`, `dominated`, `revenge`, `wipe`, `noreplay`.
Поля `player_hurt`: `userid`, `userid_pawn`, `attacker`, `attacker_pawn`, `health`, `armor`, `dmg_health`, `dmg_armor`,
`hitgroup`, `weapon`. Пустая ссылка на pawn — `-1`.

Какие игровые события есть в демке FACEIT (матч 16:12):
- раунд и матч: `begin_new_match`, `round_freeze_end`, `round_officially_ended`, `round_prestart`, `round_poststart`,
  `round_announce_*`, `cs_win_panel_match`, `announce_phase_end`, `cs_pre_restart`, `buytime_ended`;
- игроки: `player_death`, `player_hurt`, `player_blind`, `player_spawn`, `player_footstep`, `player_team`,
  `player_connect`, `player_connect_full`, `player_disconnect`, `other_death` (курицы);
- оружие: `weapon_fire`, `weapon_reload`, `weapon_zoom`, `item_equip`, `item_pickup`;
- гранаты: `flashbang_detonate`, `hegrenade_detonate`, `smokegrenade_detonate`, `smokegrenade_expired`,
  `inferno_startburn`, `inferno_expire`;
- бомба: `bomb_*`;
- прочее: `server_cvar`, `show_survival_respawn_status`.

## Особенности демок FACEIT

- **Начало.** Демка начинается в предматче: `IsMatchStarted` = true, разминки нет, играется «раунд» (в обеих демках
  он кончился победой CT). Затем разминка, затем `MatchStart` и сам матч. Всё до последнего `MatchStart`
  выбрасываем, иначе убийства предматча попадут в статистику.
- **Конец.** В последнем тике матча `GamePhase` становится 5 до `RoundEnd`, после `RoundEnd` идёт
  `MatchStartedChanged` → `false`. Самоубийства после конца матча (были в демке 13:6) FACEIT не считает,
  а табло игры считает.
- **Выход игрока с сервера живым** — `Kill` с убийцей, равным жертве, оружие `world`. FACEIT считает это смертью.
  Отличить от самоубийства командой можно по сырому `player_disconnect` с тем же `userid` (младший байт, как
  у библиотеки) в том же тике. У людей порядок такой: сырой `player_disconnect` → `Kill` → `PlayerDisconnected`.
  У выкинутого бота: `PlayerDisconnected` → сырой `player_disconnect` (reason 3) → `Kill`. У людей reason 2, 55, 158.
  Пятью демками подтверждено:
  - выходы живым почти все в заморозке, посреди раунда — один;
  - вернувшийся в заморозке игрок к её концу снова жив и играет раунд;
  - вышедший мёртвым даёт только `player_disconnect`, без `Kill`;
  - выходы после конца матча (`IsMatchStarted` уже false) не считаются.
- **Бот вместо вышедшего игрока.** Когда игрок возвращается, бота выкидывают и приходит `Kill` бота
  «самим собой» (`world`). У бота `SteamID64` = 0, в сводку боты не попадают.
- **Захват бота.** Умерший игрок управляет ботом своей команды. В `player_death` и `player_hurt` по этому боту
  `userid` указывает на игрока-контроллёра, а `userid_pawn` — на pawn бота. Библиотека берёт жертву по `userid`:
  смерть бота засчитывается игроку, а урон по боту считается от здоровья уже мёртвого игрока и выходит 0.
  Игра и FACEIT считают смерть боту, а урон — полностью. В матче 13:6 это дало лишнюю смерть одному игроку
  и −46 и −54 урона двум другим. Поэтому жертву берём по `userid_pawn` через `FindByPawnHandle`, а здоровье
  до попадания считаем сами (`demo.collector.healthTaken`). Убийцу и ассистента оставляем по `userid` —
  за игрока, управляющего ботом. Убийств под ботом в наших демках не было, на FACEIT это не проверено.
  Для тренерских метрик (METRICS.md) бот под управлением игрока — сам игрок, поэтому в журнале событий
  (`demo.Kill`, `demo.Hit`) у жертвы-бота стоит `Ref.Controller` — SteamID управляющего игрока.
  События `bot_takeover` в демках FACEIT нет, но свойство контроллёра работает: `Player.IsControllingBot()`
  и `ControlledBot()` показали захват в тот же момент, что и `player_hurt`.
- **Что встретилось в пяти демках** (после старта матча; первые две — шаг 4, ещё три — шаг 5):
  - убийства людей, ассисты, флеш-ассисты (2, 5, 1, 4, 7);
  - ассист на смерть союзника — 2 (в двух демках), FACEIT его не считает;
  - смерти от бомбы — 3, 1, 2, 0, 0, всегда после `RoundEnd`;
  - выход с сервера живым — 2 и 1 в первых двух демках, в новых ни одного; один из трёх — посреди раунда;
  - выкинутые боты — 3 и 1;
  - захват бота: 1 смерть, 4 попадания по боту; последним живым бот не оставался;
  - урон по своим и самоурон от HE и огня;
  - добивания после победы в раунде — 2, 2, 1, 1, 0;
  - два убийства в одном тике — по разу в двух демках;
  - матч с несколькими овертаймами — 35 раундов.

  Не встретилось: тимкиллы, падение, самоубийство гранатой или командой, убийства, урон и гибель
  с участием ботов под управлением компьютера, убийства игроком под ботом. Эти правила в METRICS.md
  помечены «Не проверено».
- **Бомба.** Смерть от C4 — `Kill` без убийцы, в том же тике после `RoundEnd`.
- **Названия команд.** `ClanName()` — `team_<ник капитана>`, как `Team` в статистике FACEIT.
- **Длина матча.** `mp_maxrounds` = 24. Овертайм — по 3 раунда на сторону; в начале овертайма стороны
  не меняются, после трёх раундов меняются.
- **Табло игры и FACEIT.** Убийства совпадают. Смерти на табло включают самоубийства после матча. Ассистов
  на табло у одного игрока в каждой демке на 1–2 больше. Для статистики берём события, а не табло.
- **zstd.** Окно 2 МиБ, размер распакованного в заголовке есть: 233 и 339 МиБ на 19 и 28 раундов,
  сжатие всего ~1,4×. См. `docs/notes/zstd.md`.

## Производительность

Ryzen 5 PRO 4655G, `msgQueueSize` = 1000, разбор вместе с распаковкой:

| Где | 19 раундов | 28 раундов | Пик памяти |
|---|---|---|---|
| Windows, Go 1.27.3 | 3,7 с | 6,8 с | Go 57–61 МиБ |
| Linux (WSL 2) | 3,6 с | 5,1 с | RSS 66–67 МиБ |
| Linux, одно ядро (`taskset -c 0`, `GOMAXPROCS=1`) | 7,3 с | 10,1 с | RSS 73–74 МиБ |
| Linux, две демки параллельно | 4,3 с | 5,8 с | RSS 104 МиБ |
| Linux, две параллельно на одном ядре | 14,2 с | 17,1 с | RSS 115 МиБ |

Две параллельно в cgroup с лимитами юнита (`MemoryMax=400M`, `GOMEMLIMIT=320MiB`): `memory.peak` 98 МиБ.

## Ссылки

- https://github.com/markus-wa/demoinfocs-golang — README, `examples/`, `docs/game-events.md`
- https://github.com/markus-wa/demoinfocs-golang/releases — заметки к `v6.0.0-rc.0` и `v6.0.0-rc.1`
- https://pkg.go.dev/github.com/markus-wa/demoinfocs-golang/v6/pkg/demoinfocs
- Исходники: `parsing.go` (`ParseToEnd`, ошибки), `game_events.go` (`player_death`, `player_hurt`, порядок
  `GenericGameEvent`), `datatables.go` (`bindGameRules`: воссоздание раундов), `common/player.go`
