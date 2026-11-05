Сюда кладутся свежие демки FACEIT в формате `.dem.zst` для проверки парсера и метрик (сейчас семь),
с именами, как у FACEIT: `<match_id>-1-1.dem.zst`. Здесь же `scope.json` — цифры Scope.gg для сверки
(`go run ./cmd/parsedemo -scope testdata/scope.json testdata`).
В git из этой папки попадает только этот README (см. `.gitignore`): в демках и в `scope.json` ники и SteamID.
