# Деплой бота на сервер

Бот работает как systemd-сервис на VPS за рубежом. Бинарник собираем у себя (статический, под Linux),
копируем на сервер и перезапускаем сервис. Кода на сервере нет.

Сервер: Ubuntu 24.04, x86_64, systemd 255, 1 ГБ RAM, диск 19 ГБ. На нём же работает VPN —
Xray в Docker на порту 443. Сайт и почта живут отдельно, на хостинге AdminVPS: там нет root
и systemd, бот туда не ставится.

- Docker и Xray не трогаем. Бот не слушает портов (22 — sshd, 443 — Xray): при long polling
  он сам ходит в Telegram, входящие соединения ему не нужны, firewall не меняем.
- Памяти мало, поэтому у бота в юните свои лимиты, а при нехватке памяти система убивает бота,
  а не VPN (раздел 3.2).

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

Что ещё меняем на сервере, кроме бота:

| Путь | Что это |
|---|---|
| `/swapfile` | swap на 2 ГБ (раздел 3.1) |
| `/etc/sysctl.d/99-swappiness.conf` | `vm.swappiness=10` (раздел 3.1) |
| `/etc/ssh/sshd_config.d/00-keys-only.conf` | вход по SSH только по ключу (раздел 1) |

## 1. SSH-доступ (один раз)

IP сервера в репозиторий не пишем: дальше везде алиас `fragmentor-vps`. Он указывает на VPS
за рубежом, где работает VPN, а не на хостинг AdminVPS.

1. Ключ, если его ещё нет: `ssh-keygen -t ed25519`. Файлы — в `C:\Users\<ты>\.ssh\`.
   Пароль на ключ (passphrase) — по желанию: с ним украденный файл ключа бесполезен.
2. Публичный ключ на сервер (спросит пароль от сервера в последний раз):
   ```powershell
   type $env:USERPROFILE\.ssh\id_ed25519.pub | ssh root@<IP VPS> "umask 077; mkdir -p ~/.ssh; cat >> ~/.ssh/authorized_keys"
   ```
   В Git Bash вместо `type $env:USERPROFILE\...` — `cat ~/.ssh/id_ed25519.pub`.
   Если заходишь не под root, вместо `root` — твой пользователь (и в алиасе ниже тоже).
3. Алиас — в файл `C:\Users\<ты>\.ssh\config` (его читают ssh и scp и в PowerShell, и в Git Bash):
   ```
   Host fragmentor-vps
       HostName <IP VPS>
       User root
   ```
4. Проверка: `ssh fragmentor-vps` входит без пароля.
5. Отключить вход по паролю — только после того, как сработал пункт 4.

   **Не закрывай текущую сессию на сервере, пока в новом окне не проверишь вход по ключу.**
   При ошибке без пароля на сервер не войти, останется только веб-консоль хостера.
   Уже открытая сессия при этом продолжает работать, и в ней всё можно откатить.

   На сервере:
   ```sh
   cat > /etc/ssh/sshd_config.d/00-keys-only.conf <<'EOF'
   PasswordAuthentication no
   KbdInteractiveAuthentication no
   PermitRootLogin prohibit-password
   EOF
   sshd -t && systemctl reload ssh
   sshd -T | grep -E '^(passwordauthentication|kbdinteractiveauthentication|permitrootlogin) '
   ```

   Должно быть `passwordauthentication no`, `kbdinteractiveauthentication no` и
   `permitrootlogin without-password`. Это старое имя `prohibit-password`, так его печатает OpenSSH 9.6
   из Ubuntu 24.04, а новые версии — уже `prohibit-password`. Смысл один: root входит только по ключу.
   Почему `00-` в имени: sshd берёт первое встреченное значение каждой настройки, а файлы
   из `sshd_config.d` читает по алфавиту. В образах Ubuntu бывает `50-cloud-init.conf`
   с `PasswordAuthentication yes`, и наш файл должен идти раньше.

   Не закрывая старую сессию, в **новом** окне на своём компьютере:
   ```
   ssh fragmentor-vps exit
   ssh -o PubkeyAuthentication=no -o PreferredAuthentications=password,keyboard-interactive fragmentor-vps
   ```
   Первая команда проходит без пароля, вторая получает `Permission denied (publickey)`.
   Только после этого старую сессию можно закрыть.

   Если что-то не так, откатить в старой сессии:
   ```sh
   rm /etc/ssh/sshd_config.d/00-keys-only.conf && systemctl reload ssh
   ```

   Теперь ключ `C:\Users\<ты>\.ssh\id_ed25519` — единственный способ войти по SSH.
   Сохрани его копию, например в менеджере паролей.

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
  (`HTTPS_PROXY` в `fragmentor.env`) или другой сервер.
- После проверки: `ssh fragmentor-vps rm check-telegram.sh`.

## 3. Первая установка

### 3.1. Swap

На 1 ГБ RAM без swap любой всплеск памяти заканчивается убийством процессов. Swap даёт запас.

На сервере сначала убедиться, что swap ещё нет:

```sh
swapon --show
ls -l /swapfile /swap.img
```

`swapon` ничего не выводит, а `ls` отвечает `No such file or directory` про оба файла.
Если swap или один из файлов уже есть — остановиться и написать мне.

```sh
fallocate -l 2G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
echo 'vm.swappiness=10' > /etc/sysctl.d/99-swappiness.conf
sysctl -p /etc/sysctl.d/99-swappiness.conf
swapon --show
free -h
```

Должно быть: `vm.swappiness = 10`, в `swapon --show` строка `/swapfile file 2G`,
в `free -h` — `Swap: 2.0Gi`.

- `vm.swappiness=10`: ядро уходит в swap, только когда RAM действительно не хватает,
  а рабочая память VPN и бота остаётся в RAM.
- `swapon: ... Invalid argument` или про дыры (`holes`) — пересоздать файл через `dd`:
  `swapoff /swapfile; rm /swapfile; dd if=/dev/zero of=/swapfile bs=1M count=2048`
  и повторить с `chmod 600 /swapfile`.
- `swapon: ... Operation not permitted` — VPS контейнерный (OpenVZ/LXC), swap в нём не создать.
  Убрать строку из `/etc/fstab`, удалить `/swapfile` и написать мне.

### 3.2. Бот

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

Сообщения, присланные боту, пока он был выключен, Telegram хранит 24 часа: после запуска бот на них ответит.

Лимиты памяти бота заданы в юните. Пока они предварительные, на шаге 4 их пересмотрим по замеру парсера:

| Порог | Значение | Что происходит |
|---|---|---|
| `GOMEMLIMIT` | 320 МиБ | Go чаще собирает мусор, чтобы удержаться ниже |
| `MemoryHigh` | 380 МиБ | ядро отбирает у бота кэш и притормаживает его |
| `MemoryMax` + `MemorySwapMax` | 400 МиБ RAM + 128 МиБ swap | дальше ядро убивает только бота, systemd поднимает его через 5 с |
| `OOMScoreAdjust` | 1000 | если память кончилась у всей системы, первым убивают бота |

### 3.3. Проверки и перезагрузка

1. Написать боту `/start`. Ответ пришёл, а в `journalctl -u fragmentor -f` появилась строка
   `user joined the waitlist` (для нового пользователя).
2. Лимиты и очередь на убийство при нехватке памяти:
   ```sh
   systemctl status fragmentor --no-pager | grep Memory
   for p in /proc/[0-9]*; do echo "$(cat $p/oom_score 2>/dev/null) $(cat $p/comm 2>/dev/null)"; done | sort -rn | head -n 5
   ```
   В строке `Memory:` — лимиты `high: 380M`, `max: 400M`, `swap max: 128M`.
   Первым в списке идёт `fragmentor` со счётом больше 1000, Xray — ниже.
3. Перед перезагрузкой убедиться, что VPN поднимется сам. Команда только читает настройки Docker:
   ```sh
   docker inspect -f '{{.Name}} restart={{.HostConfig.RestartPolicy.Name}}' $(docker ps -q)
   ```
   У контейнера Xray должно быть `restart=always` или `restart=unless-stopped`. Если `restart=no`
   и ты не уверен, что Xray запускается при загрузке как-то иначе, — не перезагружай и напиши мне.
4. `reboot`. На минуту-две пропадут и бот, и VPN. Потом `ssh fragmentor-vps` и на сервере:
   ```sh
   systemctl is-active fragmentor
   swapon --show
   sysctl vm.swappiness
   docker ps --format '{{.Names}}: {{.Status}}'
   journalctl -u fragmentor -b --no-pager -o cat
   ```
   Должно быть: `active`, `/swapfile file 2G`, `vm.swappiness = 10`, контейнер Xray в состоянии `Up`,
   а лог бота с начала загрузки — как в разделе 3.2. Бот снова отвечает на `/start`.
   Этот вывод пришли мне: по нему закрываем шаг 2.

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
`database schema version N is newer than this build supports`. Тогда нужна база из бэкапа раздела 4.
Всё, что записано после бэкапа, пропадёт.

```sh
systemctl stop fragmentor
rm -rf /var/lib/fragmentor
cp -a /var/backups/fragmentor-<дата> /var/lib/fragmentor
systemctl start fragmentor
```

## 6. Изменились юнит или настройки

- Юнит (`deploy/fragmentor.service`, в том числе лимиты памяти): скопировать на сервер
  (`scp deploy/fragmentor.service fragmentor-vps:`), затем
  `install -m 644 ~/fragmentor.service /etc/systemd/system/fragmentor.service`,
  `systemctl daemon-reload`, `systemctl restart fragmentor`.
- Настройки: `nano /etc/fragmentor/fragmentor.env`, затем `systemctl restart fragmentor`.

## 7. Полезные команды

| Команда | Что делает |
|---|---|
| `systemctl status fragmentor` | состояние, время запуска, память и лимиты, последние строки лога |
| `journalctl -u fragmentor -f -o cat` | лог в реальном времени |
| `journalctl -u fragmentor -b -1 -o cat` | лог до последней перезагрузки |
| `systemctl restart fragmentor` | перезапуск |
| `systemctl stop fragmentor` | остановка (сам не поднимется до `start` или перезагрузки) |
| `systemctl show -p NRestarts fragmentor` | сколько раз бот падал и перезапускался |
| `free -h`, `swapon --show` | память и swap всего сервера |
| `systemd-analyze security fragmentor` | оценка песочницы (в WSL с systemd 259 — 1.4 OK) |

## 8. Если что-то пошло не так

- **`telegram api ... Conflict` в логе.** Где-то ещё запущен бот с тем же токеном,
  например локально с боевым токеном. Локально работаем только с dev-ботом
  (`TELEGRAM_DEV_BOT_TOKEN`, см. README), боевой токен хранится только на сервере.
- **`get bot info: ... context deadline exceeded`.** Сервер не достучался до Telegram за 15 с.
  Бот перезапускается каждые несколько секунд и пробует снова. Проверь доступ скриптом
  из раздела 2. Если Telegram недоступен — `HTTPS_PROXY` в `fragmentor.env`
  (поддерживаются `http://`, `https://`, `socks5://`).
- **`TELEGRAM_BOT_TOKEN is not set`.** Нет файла `fragmentor.env` или строки в нём.
  Без файла systemd вообще не запустит сервис — смотри `systemctl status fragmentor`.
- **`Unauthorized`.** Неверный токен: лишние символы, кавычки, комментарий в конце строки.
- **`The kernel OOM killer killed some processes in this unit` и `Failed with result 'oom-kill'`.**
  Бот упёрся в свой лимит памяти. Ядро убило только бота, systemd поднял его через 5 с, VPN не задет.
  Если повторяется — пришли лог: лимиты пересматриваем по замерам.
- **Не пускает по SSH после отключения паролей.** Откатить в старой сессии (раздел 1, пункт 5).
  Если её уже нет — веб-консоль в панели хостера: там вход по паролю root работает,
  удалить `/etc/ssh/sshd_config.d/00-keys-only.conf` и выполнить `systemctl reload ssh`.
- **`Unknown key name ...` при `daemon-reload`.** Старый systemd не знает какую-то опцию
  песочницы и пропускает её. Бот работает, это только предупреждение.
- **Новой функции нужно писать файлы вне `/var/lib/fragmentor` или `/tmp`.**
  Запись блокирует `ProtectSystem=strict`. Нужный путь добавляется в юнит строкой `ReadWritePaths=`.
