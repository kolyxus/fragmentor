Сюда кладутся свежие демки FACEIT в формате `.dem.zst` для проверки парсера, метрик и находок,
с именами, как у FACEIT: `<match_id>-1-1.dem.zst`. Сейчас их 13: семь матчей kolyx в самой папке
и по три матча других уровней в папках `3-4lvl/` и `7-8lvl/`. Здесь же `scope.json` — цифры Scope.gg для сверки
(`go run ./cmd/parsedemo -scope testdata/scope.json testdata`).
В git из этой папки попадает только этот README (см. `.gitignore`): в демках и в `scope.json` ники и SteamID.
