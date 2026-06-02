# Docker Claude 컨테이너 내부 Telegram MCP 설정

> Host 측 Telegram 설정은 `docs/claudecode/host/telegram_claude_guide.md` 참조

---

## Host → Docker 복사 과정

`new_docker_claude.sh` 실행 시 Host 프로젝트의 Telegram 설정을 Docker 빌드 디렉토리로 복사:

```
Host: ~/code/<Project>/.telegram/      Docker 빌드: ~/code/DockerClaude/<Project>/.telegram/
├── .env               ──복사──→       ├── .env
├── access.json        ──복사──→       ├── access.json
└── approved/          ──복사──→       └── approved/
```

프로젝트에 `.telegram/`이 없으면 `python_ForMe` 템플릿을 사용 (별도 봇 토큰 설정 필요).

---

## 컨테이너 내부 경로 (Host와 다름)

| 항목 | Host (feihong) | Docker 내부 (node) |
|---|---|---|
| 사용자 | feihong | node |
| Telegram 상태 디렉토리 | `~/code/<Project>/.telegram/` | `/home/node/.telegram/` |
| TELEGRAM_STATE_DIR | `/home/feihong/code/<Project>/.telegram` | `/home/node/.telegram` |
| settings 파일 | `/workspace/.claude/settings.json` | `/workspace/.claude/settings.local.json` |
| Claude 홈 | `/home/feihong/.claude/` | `/home/node/.claude/` |

---

## docker-compose.yml 볼륨 마운트

```yaml
volumes:
  # Host의 DockerClaude/<Project>/.telegram → 컨테이너 /home/node/.telegram
  - /home/feihong/code/DockerClaude/<Project>/.telegram:/home/node/.telegram
```

이 bind mount를 통해 Host의 `DockerClaude/<Project>/.telegram/`과 컨테이너 내부 `/home/node/.telegram/`이 동기화된다.

---

## entrypoint.sh 자동 설정

컨테이너 시작 시 `entrypoint.sh`가 다음을 수행:

1. `/home/node/.telegram/.env`가 없으면 기본 템플릿에서 복사
2. `/home/node/.telegram/access.json`이 없으면 기본값 생성
3. `/workspace/.claude/settings.local.json`에 강제 설정:
   ```json
   {
     "env": {
       "TELEGRAM_STATE_DIR": "/home/node/.telegram"
     }
   }
   ```
4. `claude --dangerously-skip-permissions --channels plugin:telegram@claude-plugins-official` 실행

---

## Docker 환경 주의사항

### PATH 설정 금지

Host 프로젝트의 `.claude/settings.json`에 있는 PATH 설정이 Docker 내부로 복사되면 문제 발생:

```json
// Host용 (Docker에서는 사용 불가)
"PATH": "/home/feihong/.bun/bin:${PATH}"
```

- Docker 내부에서 `${PATH}`가 변수 치환되지 않고 문자열 그대로 사용됨
- bun 실행 경로 탐색 실패 → Telegram MCP 서버 미실행
- `new_docker_claude.sh`에서 `settings.local.json`을 제외하고, Host의 `settings.json` PATH는 Docker에 영향 없도록 처리

### TELEGRAM_STATE_DIR 미설정 시

Docker 내부에서 전역 경로 `/home/node/.claude/channels/telegram/`에 저장됨.
named volume이 `/home/node/.claude`를 덮고 있으므로 컨테이너 재생성 시 설정 유실 가능.
entrypoint.sh가 자동으로 설정하므로 수동 개입 불필요.

### 봇 토큰 갱신 시

Host에서 봇 토큰을 변경한 경우:

```bash
cp ~/code/<Project>/.telegram/.env ~/code/DockerClaude/<Project>/.telegram/.env
cd ~/code/DockerClaude/<Project> && docker compose restart
```
