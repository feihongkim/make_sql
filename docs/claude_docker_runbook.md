# Docker Claude 컨테이너 구축 런북

> **이 문서 하나만 보고 그대로 실행하면 된다.** 다른 문서를 참조할 필요 없다.
> `credentials_and_token.md`와 `telegram_in_docker.md`를 합친 것이며, 근거 설명은 뒤쪽
> "설계 근거" 절로 몰아 두었다. 절차만 필요하면 1~5절만 읽으면 된다.
>
> 마지막 검증: 2026-07-28, `~/code/apply` 프로젝트에서 전 과정 실행 확인.
> 토큰이 있는 경우와 없는 경우 **양쪽 모두** 기동 검증함.
> Claude Code 2.1.220 / telegram 플러그인 0.0.6 / Docker 호스트 uid 1000(feihong).

---

## 0. 전제와 절대 규칙

**구성**: 컨테이너 1개 = 프로젝트 1개 = Claude Code 1개 = Telegram 봇 1개.
호스트에서는 Claude Code **세션**을 실행하지 않는다. 사용자는 각 봇과 Telegram으로만 대화한다.

**핵심 아이디어**: 로그인 자격증명(`~/.claude/.credentials.json`)은 전 컨테이너가 공유하고,
봇 토큰·세션 기록 등 섞이면 안 되는 하위 경로만 프로젝트별로 덮어쓴다.

### 절대 하지 말 것

이 4가지는 전부 **에러 메시지 없이 조용히 실패**한다. 원인 찾기가 매우 어렵다.

| 금지 | 결과 |
|---|---|
| 컨테이너 **안에서** `claude` 명령 실행<br>(`docker exec <c> claude ...`) | 두 번째 텔레그램 MCP 서버가 같은 토큰으로 떠서 409 Conflict → **원래 봇이 죽는다** |
| 컨테이너 홈을 `/home/node`로 두기 | 공유 `plugins/`의 절대경로가 안 맞아 **플러그인 전체 로드 실패** → 봇이 안 뜬다 |
| 파일 단위 bind mount (`:ro` 포함) | 토큰 갱신 시 inode가 교체되어 **만료 토큰을 붙잡고 `/login` 루프** |
| `~/.claude/settings.json`에 쓰기 | 호스트 공유 경로라 **전역 설정이 덮어써지고** 컨테이너끼리 밀어낸다 |
| `network_mode: host` 사용 | 필요 없다. 원격 접속은 Tailscale로 하고 **전 컨테이너 bridge**로 통일 (3-3절) |

> 오케스트레이터(MakeSQL)에서 컨테이너에 일을 시키려면 첫 줄의 금지사항을 우회해야 한다.
> **`./abledb ask`** 를 쓴다 — 컨테이너 안에서 `claude` 를 새로 띄우지 않고, 이미 떠 있는
> 세션에 HTTP로 프롬프트를 넣고 응답을 받는다. 구성은 10절.

---

## 1. 호스트 사전 준비 (전체 통틀어 1회)

### 1-1. 텔레그램 플러그인 설치

`~/.claude/plugins/`는 전 컨테이너 공유이므로 호스트에서 한 번만 설치한다.
(세션이 아니라 CLI 명령이므로 "호스트에서 세션을 돌리지 않는다"는 원칙과 충돌하지 않는다.)

```bash
claude plugin install telegram@claude-plugins-official
claude plugin list | grep -A3 telegram        # Status: ✔ enabled 확인
```

이미 설치돼 있어도 아래에 해당하면 **재설치**한다:

```bash
# 마켓플레이스 개편으로 캐시가 고아 처리됐는지 확인 (있으면 봇이 조용히 안 뜬다)
ls -a ~/.claude/plugins/cache/claude-plugins-official/telegram/*/ | grep orphaned
```

### 1-2. 최초 로그인

`~/.claude`가 전 컨테이너 공유이므로 아무 컨테이너에서 한 번만 하면 된다.
컨테이너를 아직 안 만들었다면 3절까지 끝낸 뒤 돌아온다.

```bash
cd ~/code/<Project>
docker compose run --rm -it claude claude     # entrypoint 대신 claude 직접 실행
# 세션 안에서
/login
```

디렉토리 단위 마운트이므로 **다른 컨테이너를 재시작할 필요가 없다.**
이후 토큰 갱신(약 8시간 주기)도 컨테이너가 스스로 처리하고 전 컨테이너에 반영된다.

---

## 2. 봇 만들기 (프로젝트마다, 사람이 해야 함)

> **텔레그램이 필요 없는 프로젝트라면 이 절을 건너뛰어도 된다.**
> 3절의 `tele.token` 파일이 없거나 비어 있으면 텔레그램 없이 컨테이너가 만들어지고,
> Claude Code는 정상 기동한다. 나중에 토큰을 넣고 재빌드하면 그때 활성화된다 (7절).

### 2-1. BotFather

Telegram에서 [@BotFather](https://t.me/BotFather)에게 `/newbot` → 이름·username 지정 →
토큰 수령:

```
1234567890:ABCdefGHIjklMNOpqrsTUVwxyz-12345678
```

**컨테이너마다 봇을 새로 만들어야 한다.** Telegram은 토큰 하나당 `getUpdates` 소비자를
정확히 1개만 허용해서, 같은 토큰을 두 컨테이너가 물면 나중 쪽이 `409 Conflict`로 죽는다.

### 2-2. 본인 user id 확인

봇에게 아무 메시지나 보낸 뒤:

```bash
curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates" | python3 -m json.tool | grep -A3 '"from"'
```

`"id": 7723743534` 형태의 값. 봇이 달라도 user id는 동일하다.

> **컨테이너를 띄운 뒤에는 이 명령을 쓰지 말 것.** 봇이 폴링 중이면 대기 메시지를 가로채
> 컨테이너가 못 받는다. 기동 후 상태 확인은 5절 방법을 쓴다.

---

## 3. 프로젝트 파일 생성

### 3-1. 봇 토큰 파일 (선택)

토큰은 **프로젝트 루트의 `tele.token`** 에서 읽는다. 토큰 문자열만 한 줄로 넣는다.

```bash
PROJECT=apply
echo '1234567890:ABCdef...' > ~/code/$PROJECT/tele.token
```

**이 파일이 없거나 비어 있으면 텔레그램 없이 컨테이너가 만들어진다.** 그 경우에도 Claude Code는
정상 기동하고, 텔레그램 플러그인만 비활성으로 뜬다. 아래 스크립트는 두 경우를 자동으로 처리한다.

### 3-2. 생성 스크립트

**MakeSQL 저장소의 스크립트 한 줄이면 된다.** 예전에는 이 절에 230줄짜리 블록을
복붙했고, agentapi 배선(10-4)은 그 뒤에 손으로 7군데를 덧대야 했다. 지금은 스크립트가
둘을 함께 만든다.

```bash
cd ~/code/MakeSQL
./scripts/create_claude_container.sh <PROJECT> [USER_ID]

# 예
./scripts/create_claude_container.sh apply 7723743534   # 텔레그램 + agentapi
./scripts/create_claude_container.sh myproj             # agentapi 만 (tele.token 없을 때)
```

| 인자 | 뜻 |
|---|---|
| `PROJECT` | `~/code` 아래 폴더명, 또는 절대경로 (`/data2/rstudio` 처럼) |
| `USER_ID` | 2-2 에서 확인한 텔레그램 id. 생략하면 텔레그램 없이 구성 |
| `CLAUDE_VERSION=` | 환경변수로 버전 지정 (`npm view @anthropic-ai/claude-code version`) |

생성되는 것:

```
Dockerfile
docker-compose.yml
.gitignore
docker-config/entrypoint.sh
docker-config/claude.json
docker-config/managed-settings.json               ← 채널 allowlist 승인 (10-2)
docker-config/claude-settings.json                telegram + agentapi
docker-config/claude-settings-notelegram.json     agentapi 만
docker-config/claude-settings-oneshot.json        ask --new 전용, 둘 다 비활성
docker-config/telegram/{.env,access.json}
.claude-state/{channels,projects,sessions,...}    런타임 상태 디렉토리 8개
```

**여러 번 돌려도 안전하다.** 매번 완전한 세트를 생성하므로 agentapi 배선이 빠지거나
덮어써지지 않는다. 설정을 바꾼 뒤 다시 돌려도 된다.

스크립트가 만들지 않는 것은 **`tele.token` 하나**다. 봇은 사람이 만들어야 한다(2절).
토큰 없이 돌리면 텔레그램 없는 구성이 나오고, 나중에 토큰을 넣고 다시 돌리면 활성화된다.

기동 전 사전 점검도 스크립트가 한다 — agentapi 플러그인이 호스트에 설치돼 있지 않으면
설치 명령을 안내하고 멈춘다 (10-3).

> 각 파일이 왜 그렇게 생겼는지는 8절(설계 근거)과 10-2(채널 allowlist)에 있다.
> 스크립트를 고칠 일이 있으면 그쪽을 먼저 읽는다.

### 3-3. 네트워크: 전부 bridge. `network_mode: host`는 쓰지 않는다

원격 접속은 **Tailscale로 처리**하므로 host 네트워크가 필요 없다.
compose가 만드는 기본 bridge 네트워크를 그대로 쓴다 (위 템플릿에 별도 설정 없음).

bridge 컨테이너에서도 tailnet에 그대로 닿는다. 호스트가 tailnet에 붙어 있으면 컨테이너 트래픽이
호스트를 게이트웨이 삼아 `tailscale0`으로 나가고, 호스트가 masquerade 해 준다.
Docker가 호스트의 `search` 도메인을 물려주므로 **MagicDNS 짧은 이름도 해석된다.**

검증 (apply 컨테이너, bridge `172.23.0.2`):

```
goserver (100.82.67.31)  TCP/22   도달 OK
feisa    (100.122.50.38) TCP/22   도달 OK
getent hosts goserver  → 100.82.67.31  goserver.tail5b4272.ts.net
https://api.telegram.org / api.anthropic.com  응답 OK
```

> 컨테이너 안에서 `/dev/tcp/<호스트명>/<포트>`로 확인하면 IPv6를 먼저 잡아 실패할 수 있다.
> 외부 접속 확인은 `curl`을 쓴다.

SSH 키가 필요하면 Dockerfile에 추가한다 (네트워크 설정은 그대로 bridge):

```dockerfile
RUN mkdir -p /home/feihong/.ssh && chmod 700 /home/feihong/.ssh
COPY docker-config/ssh/id_ed25519 /home/feihong/.ssh/id_ed25519
RUN chmod 600 /home/feihong/.ssh/id_ed25519 && chown -R feihong:feihong /home/feihong/.ssh
RUN echo "StrictHostKeyChecking no" >> /etc/ssh/ssh_config
```

---

## 4. 빌드 및 기동

```bash
cd ~/code/$PROJECT
docker compose up -d --build

# 어느 모드로 떴는지 확인
docker compose logs --no-color | grep entrypoint
#   [entrypoint] 텔레그램 채널 활성화        ← 토큰 있음
#   [entrypoint] 봇 토큰 없음 → 텔레그램 없이 기동  ← 토큰 없음
```

아직 로그인한 적이 없다면 여기서 1-2절을 수행한다.

---

## 5. 검증

**전부 컨테이너 밖에서 실행한다.** 안에서 `claude`를 실행하면 봇이 죽는다.

```bash
PROJECT=apply

# 0) 기동 모드 확인
docker compose logs --no-color | grep entrypoint

# 1) 텔레그램 서버 프로세스 — 가장 먼저 볼 것
#    텔레그램 모드: 아래 둘 다 나와야 정상
#    텔레그램 없는 모드: 둘 다 없어야 정상 (있으면 플러그인 비활성화가 안 된 것)
docker top ${PROJECT}_claude | grep "bun server.ts"
ls ~/code/$PROJECT/.claude-state/channels/telegram/bot.pid

# 2) 실제 폴링 여부 — 409 가 나와야 정상
#    ⚠️ 이 호출은 봇의 long-poll 을 강제로 끊는다. **연속으로 여러 번 하지 말 것.**
#    플러그인은 409 를 8회까지만 재시도하고 그 다음에는 폴링을 영구히 포기한다
#    (server.ts: `if (is409 && attempt >= 8) ... Exiting`). 확인은 1회만 하고,
#    ok:true 가 나오면 먼저 `docker compose restart` 후 다시 1회만 확인한다.
curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates?timeout=1"
#  {"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates..."}
#  → 컨테이너 봇이 물고 있다는 뜻. 정상.
#  ok:true 로 메시지가 나오면 봇이 죽은 것이고, 이 호출이 대기 메시지를 가로챈 것이다.

# 2-1) agentapi 채널 (10절을 적용한 경우)
docker exec ${PROJECT}_claude curl -sS http://127.0.0.1:8799/health
#  {"ok":true,"busy":false,"queued":0}

# 3) 버전이 의도한 값인지
docker exec ${PROJECT}_claude claude --version               # claude --version 은 세션이 아니라 안전
npm view @anthropic-ai/claude-code version                   # 최신 버전과 비교

# 4) 전역 자격증명이 보이는지 (호스트와 값이 같아야 정상)
docker exec ${PROJECT}_claude head -c 120 /home/feihong/.claude/.credentials.json
head -c 120 ~/.claude/.credentials.json

# 5) 봇 토큰이 프로젝트 것인지 (컨테이너마다 달라야 정상)
docker exec ${PROJECT}_claude cat /home/feihong/.claude/channels/telegram/.env

# 6) 전역 경로가 오염되지 않았는지 (비어 있어야 정상)
ls -A ~/.claude/channels/telegram/ 2>/dev/null

# 7) 호스트 settings.json 이 컨테이너 값으로 덮이지 않았는지
cat ~/.claude/settings.json

# 8) 플러그인 로드 상태 — 반드시 호스트에서 (plugins/ 가 공유라 결과는 같다)
claude plugin list | grep -A3 telegram
```

마지막으로 봇에게 메시지를 보내면 해당 컨테이너의 Claude가 응답한다.
페어링 코드 입력 절차는 없다 (allowlist에 미리 등록했기 때문).

---

## 6. 트러블슈팅

### 봇이 조용히 응답을 멈췄다 / 처음부터 안 뜬다

로그에 에러가 안 남는 경우가 대부분이다. 이 순서로 본다.

1. **컨테이너 안에서 `claude`를 실행한 적이 있는가?**
   `docker exec <c> claude -p ...`, `claude plugin list`, `claude mcp list` 전부 해당.
   → `docker compose restart`로 복구된다. 가장 흔한 원인이다.

2. **홈 경로 불일치.** 호스트에서 `claude plugin list`가 아래처럼 나오면:
   ```
   Error: Marketplace claude-plugins-official failed to load: cache-miss
   ```
   Dockerfile의 `usermod -l feihong -d /home/feihong -m node`가 빠졌다.

3. **플러그인 고아 처리.**
   ```bash
   ls -a ~/.claude/plugins/cache/claude-plugins-official/telegram/*/ | grep orphaned
   ```
   있으면 호스트에서 `claude plugin install telegram@claude-plugins-official`.

4. **`bot.pid`는 있는데 응답이 없다.** 토큰 또는 allowlist 확인 (5절 5번, 그리고
   `.claude-state/channels/telegram/access.json`의 `allowFrom`).

### 컨테이너가 기동 직후 멈춰 있다

`docker compose logs`에 "Is this a project you created or one you trust?"가 보이면
`docker-config/claude.json`의 `projects["/workspace"].hasTrustDialogAccepted`가 빠진 것이다.

### 409 Conflict가 계속 난다

- 같은 토큰을 다른 컨테이너/프로세스가 물고 있다. 봇을 새로 만들어 분리한다.
- 호스트에서 `curl .../getUpdates`를 돌리고 있어도 충돌한다.
- 잔존 프로세스는 `bot.pid`로 정리되므로 재시작으로 대부분 해결된다.

### `Auto-update failed: no write permission to npm prefix`

npm 전역 경로가 root 소유라 컨테이너 사용자가 못 쓴다. `claude-settings.json`의
`env.DISABLE_AUTOUPDATER=1`로 끈다. 버전은 Dockerfile `CLAUDE_VERSION`으로 올린다.

### 새 모델이 `/model` 목록에 없다

컨테이너 Claude Code 버전이 낮다. `npm install -g @anthropic-ai/claude-code`를 버전 없이
쓰면 Docker 레이어 캐시 때문에 재빌드해도 안 올라간다. `CLAUDE_VERSION`을 바꿔 재빌드한다.
(실제로 컨테이너가 2.1.197에 묶여 Opus 5가 목록에 없던 사례가 있다.)

### `/login` 루프가 돈다 / 토큰이 계속 만료된다

`.credentials.json`을 파일 단위로 마운트했거나 `:ro`를 붙였다. 디렉토리 단위 마운트만 쓴다.

---

## 7. 운영

### 나중에 텔레그램 켜기 / 끄기

토큰 없이 만든 컨테이너에 나중에 봇을 붙이려면, 토큰 파일을 채우고 3-2절 스크립트를 다시 돌린 뒤
재빌드한다. entrypoint가 알아서 텔레그램 모드로 뜬다.

```bash
echo '1234567890:ABCdef...' > ~/code/<Project>/tele.token
cd ~/code/MakeSQL
./scripts/create_claude_container.sh <Project> <USER_ID>     # 재실행은 안전하다
cd ~/code/<Project> && docker compose up -d --build
docker compose logs --no-color | grep entrypoint             # "텔레그램 채널 활성화" 확인
```

끄려면 반대로 `tele.token`을 비우고(`: > tele.token`) 같은 절차를 밟는다. 이때
`.claude-state/channels/telegram/.env`에 남은 옛 토큰도 지운다 — 지우지 않으면 파일은 남지만
플러그인이 비활성이라 사용되지는 않는다.

### 봇 토큰 교체

entrypoint는 런타임 파일이 이미 있으면 덮어쓰지 않는다. 두 곳을 함께 고친다.

```bash
vi ~/code/<Project>/tele.token                             # 원본
vi ~/code/<Project>/.claude-state/channels/telegram/.env   # 실제 사용되는 파일
vi ~/code/<Project>/docker-config/telegram/.env            # 이미지 기본값
docker compose -f ~/code/<Project>/docker-compose.yml restart
```

### Claude Code 버전 올리기

```bash
npm view @anthropic-ai/claude-code version
sed -i 's/^ARG CLAUDE_VERSION=.*/ARG CLAUDE_VERSION=<새버전>/' Dockerfile
docker compose up -d --build
```

### Claude Code 업데이트 후 점검 (중요)

이 설계는 **compose에 명시한 경로만 격리**한다. 새 버전이 `~/.claude` 밑에 새 상태 디렉토리를
만들면 **조용히 다시 전 컨테이너 공유가 된다.**

```bash
ls -A ~/.claude          # 8절 표에 없는 새 디렉토리가 생겼는지
```

격리가 필요하면 compose에 한 줄 추가하고 `.claude-state/`에 디렉토리를 만든다.

### 플러그인 업데이트

`plugins/`가 공유라 **한 번 올리면 전 컨테이너에 동시 적용된다.** 호스트에서 실행하고,
이후 컨테이너 하나를 골라 5절 검증을 돌려 확인한다.

---

## 8. 설계 근거

### 8-1. 왜 `~/.claude`를 통째로 공유하나

`.credentials.json`은 Claude 설정 디렉토리 **밖으로 뺄 수 없다.** 자격증명만 따로 공유하려면
파일 단위 마운트가 필요한데, 그건 8-2 때문에 못 쓴다. 그래서 디렉토리를 통째로 공유하고,
섞이면 안 되는 하위 경로만 프로젝트 것으로 덮어쓴다. Docker는 마운트를 목적지 경로 깊이순으로
처리하므로 중첩 마운트가 성립한다 (실제 컨테이너로 검증함).

```
컨테이너 /home/feihong/.claude/        →  호스트 ~/.claude/            (전역 공유)
                    ├── .credentials.json    ← 공유. 이 설계의 목적
                    ├── plugins/             ← 공유
                    ├── agents/              ← 공유
                    │
                    ├── channels/    → <Project>/.claude-state/channels/    (덮어씀)
                    ├── projects/    → <Project>/.claude-state/projects/    (덮어씀)
                    └── ...
```

| 경로 | 처리 | 이유 |
|---|---|---|
| `.credentials.json` | **공유** | 이 설계의 목적. 한 번 로그인하면 전 컨테이너가 쓴다 |
| `plugins/` | **공유** | 컨테이너마다 받으면 디스크·네트워크 낭비. 대신 버전이 한 번에 바뀐다 |
| `agents/` | **공유** | 공용 에이전트 정의 |
| `cache/`, `backups/`, `stats-cache.json` | **공유** | 섞여도 무해 |
| `history.jsonl` | **공유** | 이력이 섞이지만 동작에는 지장 없음. 파일이라 격리하려면 파일 마운트가 필요해 포기 |
| `channels/` | **격리** | **봇 토큰. 섞이면 봇이 하나만 살아남는다** |
| `projects/`, `sessions/` | 격리 | 세션·대화 기록 |
| `shell-snapshots/`, `file-history/` | 격리 | 작업 흔적 |
| `debug/`, `ide/`, `session-env/` | 격리 | 컨테이너별 런타임 상태 |
| `settings.json` | 격리 (마운트 아님) | 8-4 참조 |

### 8-2. 왜 파일 단위 마운트를 금지하나

Claude Code는 상태 파일을 고쳐 쓰지 않고 **새 파일을 만들어 갈아끼운다**(atomic rename).
`.credentials.json`이 대표적이다 — 토큰이 약 8시간마다 갱신될 때마다 파일이 교체된다.

- 파일 하나만 bind mount 하면 컨테이너는 교체 전의 옛 inode를 계속 붙잡는다 → 만료 토큰 사용
  → `/login` 루프. `:ro`까지 붙이면 갱신 자체가 실패한다.
- 디렉토리를 mount 하면 경로로 매번 해석되므로 교체된 새 파일이 그대로 보인다.

그래서 이 설계에는 파일 단위 마운트가 하나도 없다. 컨테이너 전용 파일이 필요하면
**이미지에 굽거나(COPY), CLI 플래그로 넘긴다.**

### 8-3. 왜 컨테이너 홈을 `/home/feihong`으로 맞추나

`~/.claude` 안의 JSON들이 **절대경로를 저장한다.**

```json
// ~/.claude/plugins/known_marketplaces.json
"installLocation": "/home/feihong/.claude/plugins/marketplaces/claude-plugins-official"
```

컨테이너 홈이 `/home/node`이면 이 경로가 존재하지 않아 **플러그인 전체가 로드 실패**한다
(`cache-miss`). 텔레그램 플러그인도 포함되므로 봇이 안 뜬다. 로그에는 아무것도 안 남는다.

`usermod -l feihong -d /home/feihong -m node`로 기본 사용자를 개명한다. uid/gid는 1000 그대로라
호스트 파일 소유권도 맞는다 (호스트 feihong도 1000, 컨테이너 node도 1000).

### 8-4. 왜 settings.json을 마운트하지 않나

`~/.claude`가 호스트 공유이므로 그 안에 settings.json을 쓰면 **호스트 전역 설정이 덮어써지고**
컨테이너들이 같은 파일을 서로 밀어낸다. 대신 CLI로 넘긴다:

```bash
claude --settings /home/feihong/.defaults/settings.json ...
```

`--settings`는 기존 설정 **위에 얹히므로** 호스트 전역 설정(권한 allowlist 등)은 살아 있고
컨테이너 전용 값(model, telegram 플러그인)만 덮인다. 검증: 컨테이너 기동 후에도 호스트
`settings.json`의 `"model": "opus"`가 그대로 유지됐다.

`/home/feihong/.defaults/`는 `.claude` 밖이라 볼륨 마운트에 덮이지 않는다.

### 8-5. 왜 `TELEGRAM_STATE_DIR`을 쓰지 않나

플러그인 서버는 `STATE_DIR = $TELEGRAM_STATE_DIR ?? ~/.claude/channels/telegram`으로 동작한다.
예전에는 `channels/`가 전역 공유라 봇이 섞이는 것을 막으려고 이 환경변수로 토큰을 다른 폴더로
빼돌렸다. 그런데 **서버만 이 변수를 알고 `/telegram:access`·`/telegram:configure` 스킬은
경로를 하드코딩**해서, 스킬을 쓰면 엉뚱한 파일이 만들어졌다.

이 설계는 `channels/` 자체가 프로젝트 전용이므로 토큰을 빼돌릴 이유가 없다.
기본 경로를 그대로 쓰고, **스킬도 정상 동작한다.**

### 8-6. 왜 컨테이너 안에서 `claude`를 실행하면 안 되나

텔레그램 플러그인은 MCP 서버다(`bun server.ts`). 컨테이너 안에서 `claude`를 또 실행하면
그 프로세스도 같은 봇 토큰으로 **두 번째 텔레그램 서버를 띄운다.** Telegram은 토큰당 폴링을
1개만 허용하므로 409 Conflict가 나고 원래 채널 서버가 죽는다.

**stdout/stderr/로그 어디에도 흔적이 안 남는다.** 봇이 그냥 조용히 멈춘다.
실제로 이것 때문에 정상 동작하던 컨테이너가 죽었고 원인 파악에 시간이 걸렸다.

예외: `claude --version`은 세션을 열지 않으므로 안전하다.
`claude plugin list`, `claude mcp list`, `claude -p`는 전부 위험하다.

### 8-7. 텔레그램 없이 기동할 때 왜 `enabledPlugins: false`가 필요한가

플러그인은 **호스트에서 user scope로 설치**되고 `~/.claude/plugins/`가 공유이므로,
모든 컨테이너에서 기본 활성 상태다. 따라서 `--channels`를 안 줘도 Claude Code가 플러그인의
MCP 서버(`bun server.ts`)를 띄운다. 그 서버는 `~/.claude/channels/telegram/.env`를 읽어
**봇 토큰을 물어버린다.**

실제로 `--channels`만 빼고 테스트했더니 `bun server.ts`가 그대로 떴다.
`settings-notelegram.json`에 아래를 명시해야 서버가 안 뜬다.

```json
"enabledPlugins": { "telegram@claude-plugins-official": false }
```

명시 후 재테스트: `bun server.ts` 0개, 봇 토큰도 물지 않음(`getUpdates`가 409 대신 정상 응답).

---

## 9. 참고: 계정 정보

| 항목 | 값 (2026-07-28 확인) |
|---|---|
| subscriptionType | pro |
| rateLimitTier | default_claude_ai |
| accessToken 수명 | 약 8시간 |
| refreshToken | 있음 (컨테이너가 스스로 갱신) |
| Opus 5 사용 | 가능 (`--model claude-opus-5` 응답 확인) |

컨테이너 기본 모델은 `docker-config/claude-settings.json`의 `model` 값이다.

---

## 10. agentapi 채널 — 오케스트레이터가 컨테이너에 일 시키기

> 검증: 2026-07-28, `apply_claude`에서 telegram + agentapi 2채널 동시 등록 및 왕복 확인.

### 10-1. 왜 필요한가

MakeSQL이 개별 프로젝트 컨테이너에 요청하고 결과를 받으려면 두 가지가 필요하다.

1. **기존 세션의 문맥이 유지될 것**
2. **텔레그램 봇을 죽이지 않을 것** (0절 첫 번째 금지사항)

예전의 `./abledb docker-claude` 와 `./abledb send` 는 둘 다 `docker exec <c> claude -p` 라서 두
조건을 모두 어겼다. 새 프로세스라 문맥이 끊기고, 같은 봇 토큰으로 두 번째 텔레그램 MCP 서버가
떠서 409 Conflict 가 났다. 게다가 `-u node` 를 박아 둬서 런북 컨테이너에서는 실행조차 안 됐다.
**둘 다 제거했고 `ask` 로 통합했다** (일회성 실행이 필요하면 `ask --new`).

Claude Code의 **channel** 이 정답이다. channel 은 `experimental['claude/channel']` 을 선언한 MCP
서버로, 살아 있는 세션에 메시지를 밀어 넣고(`notifications/claude/channel`) 세션이 툴을 호출해
응답을 돌려준다. 텔레그램 플러그인이 쓰는 바로 그 구조다. `agentapi` 는 그 전송로를 HTTP 로
바꾼 것이다.

```
POST /run  →  notifications/claude/channel  →  (실행 중인 세션)  →  reply 툴  →  HTTP 응답
```

### 10-2. 채널 allowlist — 여기서 대부분 막힌다

직접 만든 채널 플러그인은 **그냥 무시된다.** Claude Code 는 승인된 채널만 등록한다.

```js
// gateChannelServer
if (!entry.dev) {
  const { entries } = allowlist(policySettings?.allowedChannelPlugins)
  if (!entries.some(l => l.plugin === entry.name && l.marketplace === entry.marketplace))
    return { action: "skip", kind: "allowlist", ... }
}
```

기본 allowlist 는 Anthropic 원격 설정(`tengu_harbor_ledger`)이고 내용은 다음 4개뿐이다.

```
claude-plugins-official 의  discord / telegram / fakechat / imessage
```

우회로는 둘인데 하나는 못 쓴다.

| 우회로 | 판정 |
|---|---|
| `--dangerously-load-development-channels` | **사용 불가.** 기동 시 확인 대화상자가 떠서 무인 컨테이너가 멈춘다 |
| managed settings 의 `allowedChannelPlugins` | 사용. 아래 10-4 |

> `allowedChannelPlugins` 는 Anthropic 기본 목록을 **대체**한다. telegram 을 빼먹으면 텔레그램
> 채널까지 같이 죽는다. `channelsEnabled: true` 를 빠뜨려도 **전 채널이 정책 차단된다.**

### 10-3. 플러그인 설치 (호스트에서 1회, 전 컨테이너 공유)

marketplace 원본 디렉토리는 **반드시 `~/.claude` 안에 둔다.** `known_marketplaces.json` 에
절대경로가 저장되는데, 컨테이너에 없는 경로면 8-3과 같은 실패가 난다
(`Marketplace ... failed to load: cache-miss` → 플러그인이 조용히 로드되지 않음).

플러그인 소스는 별도 저장소로 관리한다. **동작 중인 디렉토리가 곧 저장소**라 별도 배포 단계가
없고 사본 드리프트도 생기지 않는다.

- 저장소: `git@github.com:feihongkim/claude_extension.git` (private)
- 받는 위치: `~/.claude/marketplaces-local/makesql-channels` ← 디렉토리 이름은 이대로 유지한다
  (marketplace 이름이 `makesql-channels` 이고 플러그인 참조가 `agentapi@makesql-channels` 이다)

```bash
git clone git@github.com:feihongkim/claude_extension.git \
  ~/.claude/marketplaces-local/makesql-channels

claude plugin marketplace add ~/.claude/marketplaces-local/makesql-channels
claude plugin install agentapi@makesql-channels

# 의존성은 호스트에서 미리 받아 둔다 (컨테이너마다 bun install 하지 않도록)
cd ~/.claude/marketplaces-local/makesql-channels/plugins/agentapi && bun install
```

### 10-4. 프로젝트에 적용

**3-2 의 생성 스크립트가 이미 다 해준다.** 아래는 스크립트가 무엇을 넣는지에 대한
설명이며, 손으로 따라 할 필요는 없다. (예전에는 이 절이 7군데를 직접 고치라는
지시였고, 3-2 를 다시 돌리면 그 수정이 통째로 날아가는 문제가 있었다.)

**`docker-config/managed-settings.json`** — 이미지의 `/etc/claude-code/` 로 들어간다

```json
{
  "channelsEnabled": true,
  "allowedChannelPlugins": [
    { "marketplace": "claude-plugins-official", "plugin": "telegram" },
    { "marketplace": "makesql-channels", "plugin": "agentapi" }
  ]
}
```

> `allowedChannelPlugins` 는 Anthropic 기본 목록을 **대체**한다. telegram 을 빼면 텔레그램
> 채널까지 죽는다. `channelsEnabled: true` 를 빠뜨리면 **전 채널이 정책 차단**된다.

**`Dockerfile`** — `USER` 지시 앞에서 policy 파일을 굽는다 (root 소유여야 한다)

```dockerfile
RUN mkdir -p /etc/claude-code
COPY docker-config/managed-settings.json /etc/claude-code/managed-settings.json
```

**설정 3벌** — `enabledPlugins` 조합이 각각 다르다

| 파일 | telegram | agentapi | 쓰이는 때 |
|---|---|---|---|
| `claude-settings.json` | `true` | `true` | 토큰 있음 |
| `claude-settings-notelegram.json` | `false` | `true` | 토큰 없음 |
| `claude-settings-oneshot.json` | `false` | `false` | **`ask --new`** |

세 번째가 없으면 일회성 실행이 채널 포트를 뺏으려다 실패하고, 그 실패가 호스트 공유
캐시에 기록돼 전 컨테이너의 채널이 죽는다 (10-6).

**`entrypoint.sh`** — `--channels` 는 공백 구분 가변인자다 (`--channels <servers...>`)

```bash
# 텔레그램 있는 경우
--channels plugin:telegram@claude-plugins-official plugin:agentapi@makesql-channels
# 텔레그램 없는 경우
--channels plugin:agentapi@makesql-channels
```

### 10-5. 사용

포트는 컨테이너 내부 `127.0.0.1:8799` 에만 바인딩한다. **publish 하지 않는다.** 접근은
`docker exec` 로 하므로 별도 인증이 필요 없고, 포트 할당표도 필요 없다.

```bash
./abledb ask apply_claude "README.md 요약해줘"
./abledb ask apply_claude --file report.md --new "경고 항목만 뽑아줘"
./abledb ask tem_lms @task.txt --host alvinii --timeout 1800
```

| 옵션 | 뜻 |
|---|---|
| (기본) | **채널 모드.** 실행 중인 세션에 주입 → **문맥이 이어진다.** 큐 직렬 처리 |
| `--new` | **일회성 모드.** `claude -p` 를 새로 실행. 문맥 없음, 세션 오염 없음, 큐 무관 |
| `--file 경로` | 자료를 컨테이너 `/tmp` 로 올리고 **경로만** 프롬프트에 넣는다. 반복 지정 가능 |
| `--host 이름` | 원격 호스트의 컨테이너에 SSH 로 |
| `--timeout 초` | 기본 900 |

**모드 고르는 기준**

- 대화형 지시, 앞 작업을 이어가야 함 → **채널 모드**
- 요약·분석·정기 배치, 세션을 더럽히면 안 됨 → **`--new`**
- 큰 문서를 넘김 → **`--file` + `--new`**. 인라인(`@파일`)은 그 내용이 세션 컨텍스트에
  영구히 남는다. `--file` 은 경로만 넘기므로 에이전트가 필요한 부분만 읽는다

> `--file` 은 `docker cp` 대신 stdin 파이프를 쓴다. 그래서 로컬과 SSH 원격이 같은 코드로
> 동작하고, 작업이 끝나면 임시 디렉토리를 지운다.

직접 호출하려면:

```bash
docker exec -i <컨테이너> curl -sS -X POST http://127.0.0.1:8799/run \
  -H 'Content-Type: application/json' -d @- <<< '{"prompt":"..."}'
```

| 항목 | 값 |
|---|---|
| `POST /run` | `{"prompt": "...", "timeout_ms": 900000}` → `{"request_id","output"}` |
| `GET /health` | `{"ok","busy","queued"}` |
| 기본 타임아웃 | 900초 (`AGENTAPI_TIMEOUT_MS`) |
| 포트 | 8799 (`AGENTAPI_PORT`) |
| 동시 요청 | 큐에 쌓아 **직렬 처리**. 큐 상한 32 (`AGENTAPI_MAX_QUEUE`) |

세션이 `reply` 툴을 끝내 호출하지 않으면 타임아웃 후 504 를 돌려주고 큐를 푼다. 큐가
영구히 막히지 않는다.

### 10-6. 트러블슈팅

**`ask` 가 계속 타임아웃 (504)** — 채널이 등록되지 않은 것이다. 등록 여부는 debug 로그로 본다.

```bash
# 임시로 --debug 를 붙여 기동한 뒤
grep -E "Channel notifications|marketplace" ~/code/<Project>/.claude-state/debug/$(ls -t ~/code/<Project>/.claude-state/debug | head -1)
```

| 로그 | 원인 |
|---|---|
| `Channel notifications registered` | 정상 |
| `Marketplace makesql-channels failed to load: cache-miss` | marketplace 원본이 `~/.claude` 밖에 있다 (10-3) |
| `Channel notifications skipped: ... allowlist` | managed-settings 미적용 (10-4) |
| `Channel notifications skipped: ... policy` | `channelsEnabled: true` 누락 |
| `... not in --channels list for this session` | entrypoint 의 `--channels` 에 안 넣었다 |

**agentapi 서버가 안 뜬다** — `docker top <c> | grep "bun server.ts"` 가 2개(telegram+agentapi)여야
한다. 0~1개면 `enabledPlugins` 에 `agentapi@makesql-channels: true` 가 빠졌는지 본다.

**⚠️ 채널이 갑자기 전 컨테이너에서 안 뜬다 — `mcp-needs-auth-cache.json`**

가장 찾기 어려운 실패다. 채널 MCP 서버가 **기동 직후 비정상 종료**하면 Claude Code 는 이를
"인증이 필요한 서버"로 판단해 아래 파일에 기록하고, **그 뒤로는 서버를 아예 시작하지 않는다.**

```bash
cat ~/.claude/mcp-needs-auth-cache.json
#  { "plugin:agentapi:agentapi": { "timestamp": ..., "id": "..." } }
```

- 이 파일은 **호스트 공유 경로(`~/.claude`)** 에 있다. 한 컨테이너에서 생긴 기록이 **전 컨테이너에
  전파**된다.
- `docker compose up -d --force-recreate` 로 컨테이너를 새로 만들어도 **복구되지 않는다.**
- debug 로그에는 에러가 아니라 **`Starting connection` 줄이 통째로 사라지는** 형태로만 나타난다.
  플러그인 로드(`Checking plugin agentapi`)는 정상으로 찍히므로 더 헷갈린다.

복구:

```bash
rm -f ~/.claude/mcp-needs-auth-cache.json
docker compose restart          # 해당 컨테이너
```

원인이 된 서버 버그를 먼저 고치지 않으면 다음 기동에서 다시 기록된다. 실제 사례: 채널 서버가
`process.stdin` 에 `'end'/'close'` 리스너를 붙여 stdin 이 flowing 모드가 되는 바람에
StdioServerTransport 가 읽어야 할 메시지를 가로채 서버가 즉시 종료했다. **채널 서버에서
`process.stdin` 을 건드리면 안 된다.** 종료는 `SIGINT`/`SIGTERM` 으로만 처리한다
(Claude Code 는 MCP 서버를 내릴 때 SIGINT 를 보낸다).

**⚠️ 일회성 `claude -p` 도 플러그인 MCP 서버를 띄운다 — 전용 settings 가 필수다**

`-p`(print) 모드라고 예외가 아니다. `enabledPlugins` 가 `true` 면 플러그인 MCP 서버를 그대로
시작한다. 컨테이너 안에서 일회성 실행을 하면 **이미 떠 있는 채널 서버와 포트가 충돌**한다.

```
MCP server "plugin:agentapi:agentapi": Starting connection with timeout of 30000ms
Server stderr: agentapi: failed to listen on 127.0.0.1:8799: Is port 8799 in use?
Connection failed (-32000): MCP error -32000: Connection closed
→ ~/.claude/mcp-needs-auth-cache.json 재생성
```

그 순간 세션은 멀쩡해 보이지만, **다음 재시작부터 전 컨테이너에서 채널이 죽는다.**
(위 needs-auth 캐시 항목 참조)

따라서 일회성 실행에는 **두 플러그인을 모두 `false` 로 명시한 전용 settings** 를 쓴다.

```json
// docker-config/claude-settings-oneshot.json
{
  "model": "sonnet",
  "enabledPlugins": {
    "telegram@claude-plugins-official": false,
    "agentapi@makesql-channels": false
  },
  "skipDangerousModePermissionPrompt": true,
  "env": { "DISABLE_AUTOUPDATER": "1" }
}
```

```bash
docker exec -u feihong -w /workspace <컨테이너> \
  claude -p "<프롬프트>" --dangerously-skip-permissions \
  --settings /home/feihong/.defaults/settings-oneshot.json
```

`settings-notelegram.json` 을 재사용하면 안 된다 — 그쪽은 agentapi 가 `true` 라 위 충돌이 난다.

**플러그인 소스를 고쳤다** — 호스트에서 `claude plugin marketplace update makesql-channels` 후
해당 컨테이너 `docker compose restart`. `plugins/` 는 공유라 전 컨테이너에 동시 반영된다.
