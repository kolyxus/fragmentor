# Деплой бота на сервер

Бот работает на VPS как systemd-сервис. Бинарник собираем у себя (статический, под Linux),
копируем на сервер и перезапускаем сервис. Кода на сервере нет. Входящие порты не нужны:
при long polling бот сам ходит в Telegram.

Локальные команды — из папки `bot/`, в PowerShell или Git Bash. Серверные — под root;
если заходишь обычным пользователем, сначала `sudo -i`.

## Что где лежит на сервере

| Путь | Что это | Владелец, права |
|---|---|---|
| `/opt/fragmentor/fragmentor` | бинарник бота | root, 755 |
| `/opt/fragmentor/fragmentor.prev` | предыдущая версия для отката | root, 755 |
| `/etc/fragmentor/fragmentor.env` | настройки и секреты | root, 600 |
| `/var/lib/fragmentor/fragmentor.db` | база SQLite | fragmentor, 700 на папку |
| `/etc/systemd/system/fragmentor.service` | копия `deploy/fragmentor.service` | root, 644 |

- Бот работает от системного пользователя `fragmentor` без входа в систему.
- `fragmentor.env` читает systemd от root и передаёт боту переменными окружения.
- Папку базы systemd создаёт сам (`StateDirectory`), путь к базе задан в юните.
- Писать бот может только в `/var/lib/fragmentor` и свой `/tmp`: остальное в песочнице только для чтения.
- Логи — в journald, ротация встроенная: `journalctl -u fragmentor`.

## 1. SSH-доступ (один раз, на своём компьютере)

IP сервера в репозиторий не пишем: дальше везде алиас `fragmentor-vps`.

1. Ключ, если его ещё нет: `ssh-keygen -t ed25519`. Файлы — в `C:\Users\<ты>\.ssh\`.
2. Публичный ключ на сервер (спросит пароль в последний раз):
   ```powershell
   type $env:USERPROFILE\.ssh\id_ed25519.pub | ssh root@<IP> "umask 077; mkdir -p ~/.ssh; cat >> ~/.ssh/authorized_keys"
   ```
   В Git Bash вместо `type $env:USERPROFILE\...` — `cat ~/.ssh/id_ed25519.pub`.
3. Алиас — в файл `C:\Users\<ты>\.ssh\config` (его читают ssh и scp и в PowerShell, и в Git Bash):
   ```
   Host fragmentor-vps
       HostName <IP сервера>
       User root
   ```
4. Проверка: `ssh fragmentor-vps` входит без пароля.

## 2. Проверка доступа к Telegram (перед первой установкой)

```
scp deploy/check-telegram.sh fragmentor-vps:
ssh -t fragmentor-vps bash check-telegram.sh
```

`-t` обязателен: без терминала на сервере ввод токена не скрывается.
Или открой `deploy/check-telegram.sh` и выполни его строки на сервере по одной.
Скрипт покажет систему сервера, пять раз замерит соединение с `api.telegram.org` без токена
и сделает `getMe` с токеном. Токен вводится скрыто и не попадает ни в историю команд,
ни в список процессов.

- Норма — `total` до ~0,5 с и `getMe` с `"ok":true` и `"username":"fragmentor_ai_bot"`.
- Таймауты, ошибки или секунды на запрос — остановиться и решать: прокси
  (`HTTPS_PROXY` в `fragmentor.env`) или сервер в другой стране.
- После проверки: `ssh fragmentor-vps rm check-telegram.sh`.

## 3. Первая установка

На своём компьютере:

```
make lint test build-linux
scp bin/fragmentor-linux-amd64 deploy/fragmentor.service fragmentor-vps:
ssh fragmentor-vps
```

На сервере:

```sh
id fragmentor >/dev/null 2>&1 || useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin fragmentor
install -d -m 755 /opt/fragmentor
install -d -m 700 /etc/fragmentor
touch /etc/fragmentor/fragmentor.env && chmod 600 /etc/fragmentor/fragmentor.env
nano /etc/fragmentor/fragmentor.env
```

В `fragmentor.env` — одна строка, токен вставить вместо `...`:

```
TELEGRAM_BOT_TOKEN=...
```

- Формат: `KEY=VALUE` без кавычек, без `export` и без комментариев в конце строки
  (systemd читает их как часть значения).
- Токен вводим только в редакторе: в команде (`echo ... >`) он остался бы в истории.
- `TELEGRAM_DEV_BOT_TOKEN` здесь не задаём никогда: он важнее боевого, и сервер поднял бы dev-бота.
- По желанию: `LOG_LEVEL=debug`, `HTTPS_PROXY=...`. Остальные переменные понадобятся в следующих шагах.

Дальше:

```sh
install -m 755 ~/fragmentor-linux-amd64 /opt/fragmentor/fragmentor
install -m 644 ~/fragmentor.service /etc/systemd/system/fragmentor.service
rm ~/fragmentor-linux-amd64 ~/fragmentor.service
systemctl daemon-reload
systemctl enable --now fragmentor
journalctl -u fragmentor -n 20 --no-pager -o cat
```

В логе должно быть:

```
msg=starting version=<коммит> dev_bot=false
msg="database ready" path=/var/lib/fragmentor/fragmentor.db schema_version=1
msg="telegram bot authorized" username=fragmentor_ai_bot
msg="bot profile up to date"
msg="polling for updates"
```

Проверки:

1. Написать боту `/start`. Ответ пришёл, а в `journalctl -u fragmentor -f` появилась строка
   `user joined the waitlist` (для нового пользователя).
2. `reboot`. Через минуту — `ssh fragmentor-vps systemctl is-active fragmentor` → `active`,
   и бот снова отвечает.

Сообщения, присланные боту, пока он был выключен, Telegram хранит 24 часа: после запуска бот на них ответит.

## 4. Выкатить новую версию

На своём компьютере, из закоммиченного состояния (иначе в логе будет `version=<коммит>+dirty`):

```
make lint test build-linux
scp bin/fragmentor-linux-amd64 fragmentor-vps:
ssh fragmentor-vps
```

Есть ли в новой версии миграции: `git diff --name-only <коммит с сервера> HEAD -- internal/storage/migrations`.
Коммит, который сейчас работает на сервере, — в строке `starting version=...` его лога.
Если миграции есть, на сервере сначала бэкап базы:

```sh
systemctl stop fragmentor
cp -a /var/lib/fragmentor /var/backups/fragmentor-$(date +%Y%m%d-%H%M)
```

Замена бинарника (старый остаётся как `.prev`):

```sh
cp -a /opt/fragmentor/fragmentor /opt/fragmentor/fragmentor.prev
install -m 755 ~/fragmentor-linux-amd64 /opt/fragmentor/fragmentor.new
mv /opt/fragmentor/fragmentor.new /opt/fragmentor/fragmentor
rm ~/fragmentor-linux-amd64
systemctl restart fragmentor
journalctl -u fragmentor -n 20 --no-pager -o cat
```

В логе — `starting version=` с новым коммитом (`git rev-parse --short=12 HEAD`) и `polling for updates`.
При остановке бот дожидается ответов на начатые сообщения, поэтому `restart` безопасен.

## 5. Откат

```sh
mv /opt/fragmentor/fragmentor.prev /opt/fragmentor/fragmentor
systemctl restart fragmentor
```

Если новая версия успела применить миграцию, старая откажется стартовать с ошибкой
`database schema version N is newer than this build supports`. Тогда нужна база из бэкапа шага 4.
Всё, что записано после бэкапа, пропадёт.

```sh
systemctl stop fragmentor
rm -rf /var/lib/fragmentor
cp -a /var/backups/fragmentor-<дата> /var/lib/fragmentor
systemctl start fragmentor
```

## 6. Изменились юнит или настройки

- Юнит (`deploy/fragmentor.service`): скопировать как в шаге 3, затем
  `install -m 644 ~/fragmentor.service /etc/systemd/system/fragmentor.service`,
  `systemctl daemon-reload`, `systemctl restart fragmentor`.
- Настройки: `nano /etc/fragmentor/fragmentor.env`, затем `systemctl restart fragmentor`.

## 7. Полезные команды

| Команда | Что делает |
|---|---|
| `systemctl status fragmentor` | состояние, время запуска, последние строки лога |
| `journalctl -u fragmentor -f -o cat` | лог в реальном времени |
| `journalctl -u fragmentor -b -1 -o cat` | лог до последней перезагрузки |
| `systemctl restart fragmentor` | перезапуск |
| `systemctl stop fragmentor` | остановка (сам не поднимется до `start` или перезагрузки) |
| `systemctl show -p NRestarts fragmentor` | сколько раз бот падал и перезапускался |
| `systemd-analyze security fragmentor` | оценка песочницы (в WSL с systemd 259 — 1.4 OK) |

## 8. Если что-то пошло не так

- **`telegram api ... Conflict` в логе.** Где-то ещё запущен бот с тем же токеном,
  например локально с боевым токеном. Локально работаем только с dev-ботом
  (`TELEGRAM_DEV_BOT_TOKEN`, см. README), боевой токен хранится только на сервере.
- **`get bot info: ... context deadline exceeded`.** Сервер не достучался до Telegram за 15 с.
  Бот перезапускается каждые несколько секунд и пробует снова. Проверь доступ скриптом
  из шага 2. Если Telegram заблокирован — `HTTPS_PROXY` в `fragmentor.env`
  (поддерживаются `http://`, `https://`, `socks5://`).
- **`TELEGRAM_BOT_TOKEN is not set`.** Нет файла `fragmentor.env` или строки в нём.
  Без файла systemd вообще не запустит сервис — смотри `systemctl status fragmentor`.
- **`Unauthorized`.** Неверный токен: лишние символы, кавычки, комментарий в конце строки.
- **`Unknown key name ...` при `daemon-reload`.** Старый systemd не знает какую-то опцию
  песочницы и пропускает её. Бот работает, это только предупреждение.
- **Новой функции нужно писать файлы вне `/var/lib/fragmentor` или `/tmp`.**
  Запись блокирует `ProtectSystem=strict`. Нужный путь добавляется в юнит строкой `ReadWritePaths=`.
