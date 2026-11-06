# Деплой бота на сервер

Бот работает как systemd-сервис на VPS за рубежом. Бинарник собираем у себя (статический, под Linux),
копируем на сервер и перезапускаем сервис. Кода на сервере нет.

Сервер: Ubuntu 24.04, x86_64, systemd 255, 1 ГБ RAM, диск 19 ГБ. На нём же работает VPN —
Xray в Docker на порту 443. Сайт и почта живут отдельно, на хостинге AdminVPS: там нет root
и systemd, бот туда не ставится.

- Docker и Xray не трогаем. Бот не слушает портов (22 — sshd, 443 — Xray): при long polling
  он сам ходит в Telegram, входящие соединения ему не нужны. В firewall появляются только
  правила fail2ban для порта 22 (раздел 1.3).
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
| `/var/backups/fragmentor-<дата-время>` | копии базы перед выкаткой, `make deploy` оставляет 10 последних | fragmentor |
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
| `/etc/fail2ban/jail.d/sshd.local` | fail2ban: бан за подбор пароля к SSH (раздел 1.3) |
| `/etc/ssh/sshd_config.d/00-keys-only.conf` | вход по SSH только по ключу — необязательно, сейчас не используется (раздел 1.2) |

## 1. SSH-доступ

IP сервера в репозиторий не пишем: дальше везде алиас `fragmentor-vps`. Он указывает на VPS
за рубежом, где работает VPN, а не на хостинг AdminVPS.

### 1.1. Вход по ключу (один раз, на своём компьютере)

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

### 1.2. Отключить вход по паролю (необязательно)

На сервере бота вход по паролю оставлен включённым (решение от 2026-11-03 в `docs/DECISIONS.md`):
по паролю входят клиент Amnezia и устройства, на которых нет ключа. Это компромисс:

- **Пароль включён, как сейчас.** Войти можно с любого устройства, если знаешь пароль. Цена —
  боты со всего интернета круглосуточно подбирают пароли к SSH. Защищают длинный уникальный пароль,
  который перебором не подобрать, и fail2ban (раздел 1.3): после 5 неудач за 10 минут
  IP теряет доступ к SSH на час.
- **Только ключ.** Подбирать нечего: без файла ключа не войти, сколько ни пробуй. Цена — ключ нужен
  каждому устройству и программе, которые заходят на сервер, а потерянный ключ оставляет
  только веб-консоль хостера.

Отключать пароль имеет смысл, когда и клиент Amnezia, и все устройства будут входить по ключу.
Что сейчас на сервере: `sshd -T | grep -E '^(passwordauthentication|permitrootlogin) '`
(`passwordauthentication yes` — пароль включён).

Как отключить — только после того, как сработала проверка из раздела 1.1.

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

Ключ `C:\Users\<ты>\.ssh\id_ed25519` теперь единственный способ войти по SSH.
Сохрани его копию, например в менеджере паролей.

Вернуть вход по паролю — при ошибке в старой сессии или в любой момент позже:
```sh
rm /etc/ssh/sshd_config.d/00-keys-only.conf && systemctl reload ssh
```

### 1.3. fail2ban: бан за подбор пароля

Раз вход по паролю включён, нужен fail2ban. Он следит за логом sshd и после 5 неудачных попыток
за 10 минут закрывает этому IP порт 22 на час. Бан касается только SSH: VPN на 443 и бот работают как обычно.
Свои ошибки тоже считаются: одна неверная попытка — одна неудача, а бан ставится на IP, то есть
на все устройства за тем же роутером. Как снять бан — в разделе 8.

**Откуда fail2ban читает лог sshd.** В минимальном образе может не быть rsyslog, а значит, и файла
`/var/log/auth.log`. Это не мешает: пакет fail2ban в Ubuntu 24.04 (`1.0.2-3ubuntu0.1`) по умолчанию
берёт лог sshd из journald (`backend = systemd` в его `jail.d/defaults-debian.conf`), а путь к файлу
при этом пропускает. В нашем файле `backend = systemd` записан явно, чтобы так оставалось
и после обновлений пакета.

**Чем банит.** По умолчанию пакет банит через nftables, то есть утилитой `nft`. Пакет `nftables`
у fail2ban в рекомендуемых, причём как альтернатива `iptables` («nftables | iptables»). `iptables` уже
стоит вместе с Docker, поэтому apt считает рекомендацию выполненной и `nftables` не ставит. Без `nft`
fail2ban видит попытки, но бан падает с ошибкой. Поэтому в нашем файле `banaction = iptables-multiport`:
тот же `iptables`, которым пользуется Docker. fail2ban заводит свою цепочку `f2b-sshd` и переход
в неё из `INPUT` только для порта 22. Цепочки Docker он не трогает.

На сервере сначала проверки:

```sh
apt update
apt-cache policy fail2ban
command -v iptables nft
journalctl _COMM=sshd -n 5 --no-pager -o cat
ls -l /var/log/auth.log
```

- `Candidate: 1.0.2-3ubuntu0.1`. Версия `1.0.2-3` из самого релиза 24.04 не запускается
  на Python 3.12. Если кандидат она — остановиться и написать мне.
- `command -v` печатает путь к `iptables`. Есть ли `nft`, неважно. Если `iptables` нет —
  остановиться и написать мне.
- `journalctl _COMM=sshd` показывает последние строки sshd. Их же будет читать fail2ban:
  в его фильтре условие `_COMM=sshd`.
- `ls` может ответить `No such file or directory` — это нормально.

Установка. Файл настроек кладём до установки пакета: пакет сразу включает и запускает сервис,
и тот стартует уже с нашими настройками. Свои настройки — только в `.local`: файлы
из `jail.d` с этим расширением читаются после файлов пакета и перекрывают их, а сами файлы пакета не правим.

```sh
install -d -m 755 /etc/fail2ban/jail.d
cat > /etc/fail2ban/jail.d/sshd.local <<'EOF'
[sshd]
enabled   = true
backend   = systemd
banaction = iptables-multiport
maxretry  = 5
findtime  = 10m
bantime   = 1h
EOF
apt install -y fail2ban
systemctl enable fail2ban
systemctl restart fail2ban
```

Проверка:

```sh
systemctl is-enabled fail2ban
systemctl status fail2ban --no-pager | grep -E 'Active:|Memory:'
fail2ban-client status sshd
fail2ban-client get sshd actions
```

- `enabled` (поднимется после перезагрузки) и `Active: active (running)`. В строке `Memory:` —
  сколько занимает fail2ban: на 1 ГБ RAM рядом с VPN и ботом это важно.
- `fail2ban-client status sshd`:
  ```
  Status for the jail: sshd
  |- Filter
  |  |- Currently failed: 0
  |  |- Total failed:     0
  |  `- Journal matches:  _SYSTEMD_UNIT=sshd.service + _COMM=sshd
  `- Actions
     |- Currently banned: 0
     |- Total banned:     0
     `- Banned IP list:
  ```
  Числа могут быть уже не нулевыми — это боты. Главное — строка `Journal matches`: fail2ban
  читает journald. Если вместо неё `File list: /var/log/auth.log`, наш файл не применился:
  проверь имя и путь `/etc/fail2ban/jail.d/sshd.local`. На Ubuntu сервис называется `ssh.service`,
  а не `sshd.service`, но `+` в условии означает «или»: строки sshd ловятся по `_COMM=sshd`.
- `fail2ban-client get sshd actions` — `iptables-multiport`.

Ловит ли fail2ban неудачи. В новом окне на своём компьютере:

```
ssh -o PubkeyAuthentication=no -o PreferredAuthentications=password,keyboard-interactive fragmentor-vps
```

Один раз введи неверный пароль и нажми Ctrl+C. На сервере `fail2ban-client status sshd`:
`Total failed` вырос хотя бы на 1 (боты тоже прибавляют). Больше 4 неверных паролей за 10 минут
не вводи, иначе свой IP окажется в бане на час.

Через час-другой проверить, что баны работают:

```sh
fail2ban-client status sshd
iptables -S f2b-sshd
grep -E 'Ban |ERROR' /var/log/fail2ban.log | tail -n 5
```

`Total banned` больше 0, в `iptables -S f2b-sshd` есть строки `-j REJECT` для забаненных IP,
в логе — строки `[sshd] Ban <IP>` без `ERROR`. Цепочка `f2b-sshd` появляется при первом бане:
до него `iptables` ответит, что такой цепочки нет, — это нормально. `Total banned: 0` значит лишь,
что никто ещё не набрал 5 неудач за 10 минут: проверь позже.

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

В `fragmentor.env` — две строки: токен бота и server-side ключ FACEIT Data API вместо `...`:

```
TELEGRAM_BOT_TOKEN=...
FACEIT_API_KEY=...
```

- Формат: `KEY=VALUE` без кавычек, без `export` и без комментариев в конце строки
  (systemd читает их как часть значения).
- Токен и ключ вводим только в редакторе: в команде (`echo ... >`) они остались бы в истории.
- `TELEGRAM_DEV_BOT_TOKEN` здесь не задаём никогда: он важнее боевого, и сервер поднял бы dev-бота.
- По желанию: `LOG_LEVEL=debug`, `HTTPS_PROXY=...`.
- `ADMIN_TELEGRAM_IDS=<Telegram ID через запятую>` — кто может присылать демки на разбор до шага 12.
  Без неё бот работает, а на ссылки на демки всем отвечает, что разборы откроются с бетой.
  Свой ID показывает Telegram Desktop, если включить в экспериментальных настройках
  «Show Peer IDs in Profile». Остальные переменные понадобятся в следующих шагах.

Дальше:

```sh
install -m 755 ~/fragmentor-linux-amd64 /opt/fragmentor/fragmentor
install -m 644 ~/fragmentor.service /etc/systemd/system/fragmentor.service
rm ~/fragmentor-linux-amd64 ~/fragmentor.service
systemctl daemon-reload
systemctl enable --now fragmentor
journalctl -u fragmentor -n 20 --no-pager -o cat
```

В логе должно быть (`schema_version` — номер последней миграции в `internal/storage/migrations`):

```
msg=starting version=<коммит> dev_bot=false
msg="database ready" path=/var/lib/fragmentor/fragmentor.db schema_version=3
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

### 4.1. Одной командой: `make deploy`

На своём компьютере, из папки `bot/`, в PowerShell или Git Bash:

```
make deploy
```

Запускаешь только ты: Claude Code эта цель запрещена (`.claude/settings.json`).

Что она делает — то же, что раздел 4.2 вручную:

1. Останавливается с ошибкой `make deploy: commit or stash the changes first: ...` и списком файлов,
   если в репозитории есть незакоммиченные или неотслеживаемые изменения: иначе на сервер уехала бы
   не закоммиченная версия, а в логе было бы `version=<коммит>+dirty`.
2. `make lint test build-linux`. Упали `gofmt`, `go vet` или тесты — дальше не идёт, на сервере ничего не меняется.
3. `scp` бинарника и скрипта `deploy/install-release.sh` в домашнюю папку root на `fragmentor-vps`.
4. `ssh fragmentor-vps sh install-release.sh <коммит> fragmentor-linux-amd64`. Скрипт на сервере:
   - останавливает бота (`systemctl stop`);
   - копирует базу в `/var/backups/fragmentor-<ГГГГММДД-ЧЧММСС>` и оставляет 10 последних копий,
     поэтому проверять, есть ли в версии миграции, больше не нужно;
   - сохраняет старый бинарник как `fragmentor.prev`, ставит новый и запускает бота;
   - до 30 с ждёт в логе `starting version=<коммит>` и `polling for updates`, затем печатает лог с момента запуска;
   - убирает из домашней папки бинарник и сам скрипт.

В конце — `deploy: version <коммит> is up ...`. Бот простаивает несколько секунд: от остановки до запуска.

Если что-то пошло не так:
- **На сервере сбой до запуска новой версии** (например, не удалась копия базы): скрипт снова запускает бота
  на старом бинарнике и выходит с ошибкой.
- **`deploy: run as root: ...`** — алиас заходит не под root (`User root` в `~/.ssh/config`, раздел 1.1).
- **`deploy: the log has no 'starting version=...'` или `no 'polling for updates' in 30 s`** — новая версия
  не поднялась. Смотри лог, который скрипт напечатал выше, и раздел 8; откат — раздел 5, копия базы — в последней
  `/var/backups/fragmentor-*`.
- **`scp`/`ssh` не подключились** — проверь `ssh fragmentor-vps` (раздел 1).
- **`process_begin: CreateProcess(NULL, scp ...) failed` и `make (e=2)`** — make не нашёл `scp`. Make из Chocolatey
  32-битный и не видит `C:\Windows\System32\OpenSSH`: Windows подсовывает 32-битным программам `SysWOW64`.
  Поэтому Makefile сам берёт `ssh` и `scp` из `C:\Windows\Sysnative\OpenSSH` — так 32-битная программа видит
  настоящий `System32`. Если ошибка всё равно есть, укажи пути явно:
  `make deploy DEPLOY_SCP=<путь к scp.exe> DEPLOY_SSH=<путь к ssh.exe>`.

Другой сервер или алиас: `make deploy DEPLOY_HOST=<алиас>`. Под ARM: `make deploy LINUX_ARCH=arm64`.

### 4.2. Вручную

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
cp -a /var/lib/fragmentor /var/backups/fragmentor-$(date +%Y%m%d-%H%M%S)
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
| `fail2ban-client status sshd` | неудачные попытки входа по SSH и забаненные IP |
| `fail2ban-client set sshd unbanip <IP>` | снять бан с IP |

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
- **`FACEIT_API_KEY is not set`.** В `fragmentor.env` нет строки с ключом FACEIT (раздел 3.2),
  без неё бот не стартует.
- **`invalid ADMIN_TELEGRAM_IDS`.** В строке `ADMIN_TELEGRAM_IDS` не числа через запятую
  (раздел 3.2): лишние символы, точка с запятой. Бот не стартует.
- **`job failed ... reason=link_rejected` в логе.** Хранилище FACEIT не приняло ссылку на демку: она протухла
  (живёт 5 минут) или испорчена при копировании. Пользователь получает просьбу прислать свежую.
- **`faceit: ... status 400 (invalid_token)` в логе.** FACEIT не принял ключ: лишние символы
  или ключ отозван в App Studio. Нужен server-side ключ, не client-side.
- **`faceit: ... status 429` или `status 503`.** FACEIT ограничил частоту или недоступен. Бот сам
  повторяет запрос до трёх раз, пользователю отвечает «FACEIT не отвечает». Если повторяется
  постоянно — пришли лог.
- **`Unauthorized`.** Неверный токен: лишние символы, кавычки, комментарий в конце строки.
- **`The kernel OOM killer killed some processes in this unit` и `Failed with result 'oom-kill'`.**
  Бот упёрся в свой лимит памяти. Ядро убило только бота, systemd поднял его через 5 с, VPN не задет.
  Если повторяется — пришли лог: лимиты пересматриваем по замерам.
- **SSH с твоего IP не пускает (`Connection refused` или `Connection timed out`), а VPN работает.**
  Скорее всего, fail2ban забанил IP после неверных паролей: твоих или клиента Amnezia со старым паролем.
  Бан снимется сам через час. Сразу — с другого IP (например, с телефона через мобильный интернет)
  или из веб-консоли хостера: `fail2ban-client set sshd unbanip <твой IP>`.
  Забаненные IP показывает `fail2ban-client status sshd`.
- **В `/var/log/fail2ban.log` ошибки `nft: not found`.** Не применился `sshd.local`, и fail2ban
  банит через nftables по умолчанию. Проверь путь `/etc/fail2ban/jail.d/sshd.local`, затем
  `systemctl restart fail2ban` и `fail2ban-client get sshd actions` → `iptables-multiport`.
- **Не пускает по SSH после отключения паролей.** Откатить в старой сессии (раздел 1.2).
  Если её уже нет — веб-консоль в панели хостера: там вход по паролю root работает,
  удалить `/etc/ssh/sshd_config.d/00-keys-only.conf` и выполнить `systemctl reload ssh`.
- **`Unknown key name ...` при `daemon-reload`.** Старый systemd не знает какую-то опцию
  песочницы и пропускает её. Бот работает, это только предупреждение.
- **Новой функции нужно писать файлы вне `/var/lib/fragmentor` или `/tmp`.**
  Запись блокирует `ProtectSystem=strict`. Нужный путь добавляется в юнит строкой `ReadWritePaths=`.
