Сюда кладутся свежие демки FACEIT в формате `.dem.zst` для проверки парсера, метрик и находок,
с именами, как у FACEIT: `<match_id>-1-1.dem.zst`. Сейчас их 13: семь матчей kolyx в самой папке
и по три матча других уровней в папках `3-4lvl/` и `7-8lvl/`. Тесты берут демки из самой папки и её папок,
кроме `maps/` (`internal/testdemos`), под `-race` — три самые маленькие.

В `maps/` лежат демки карт, которых нет среди остальных: они нужны для словаря зон `content/places.yaml`,
тесты их не разбирают. Зоны из них, где каждая зона и есть ли она в словаре, печатает
`go run ./cmd/parsedemo -places testdata/maps` (раздел «Разбор демок» в `bot/README.md`). Сейчас там Dust2,
Mirage и Nuke; в демке Dust2 игроки убивали и наносили урон, управляя ботом.

Все демки отсюда и из папок, включая `maps/`, сверяет с FACEIT `make verify-demos` (раздел 4.3 `bot/DEPLOY.md`):
ID матча она берёт из имени файла, поэтому и в `maps/` имена — как у FACEIT.

Здесь же `scope.json` — цифры Scope.gg для сверки (`go run ./cmd/parsedemo -scope testdata/scope.json testdata`),
и сюда пишется вывод прогона тестов под `-race`: `go test -race -p 1 ./... > testdata/race.log 2>&1`.
В git из этой папки попадает только этот README (см. `.gitignore`): в демках и в `scope.json` ники и SteamID.
