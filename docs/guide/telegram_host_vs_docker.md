# Host vs Docker Claude — Telegram MCP 환경 분리 가이드

## 핵심 차이

| 항목 | Host (feihong) | Docker Claude (node) |
|---|---|---|
| 계정 | `feihong` | `node` |
| Claude 홈 | `/home/feihong/.claude/` | `/home/node/.claude/` |
| 기본 Telegram 상태 경로 | `/home/feihong/.claude/channels/telegram/` | `/home/node/.claude/channels/telegram/` |
| 프로젝트 로컬 상태 경로 | 프로젝트/.telegram/ | 프로젝트/.telegram/ (컨테이너 내부 경로) |

---

## 문제: 두 환경의 설정 파일 혼용

### 발생 원인

Docker Claude 컨테이너는 `/home/node/.claude/`를 사용하지만,
`docker-compose.yml`에서 Host의 credentials를 bind mount할 때:

```yaml
volumes:
  - /home/feihong/.claude/.credentials.json:/home/node/.claude/.credentials.json:ro
  - some_named_volume:/home/node/.claude
```

named volume이 `.claude/` 전체를 덮어씌우면서 Host의 `channels/telegram/` 설정이
컨테이너 내부로 흘러들어가거나, 반대로 컨테이너 설정이 Host에 영향을 줄 수 있음.

### 실제 사례

Docker Claude 컨테이너 내부에서 `/telegram:configure`를 실행하면
`TELEGRAM_STATE_DIR`이 설정되지 않은 경우 **전역 경로** `/home/node/.claude/channels/telegram/`에 저장됨.

이 경로가 bind mount나 volume으로 Host의 `~/.claude/channels/telegram/`과 연결되어 있으면
**Host Claude의 Telegram MCP 토큰이 덮어쓰여져 작동 불능** 상태가 됨.

---

## 올바른 설정 방법

### Host 프로젝트 (feihong)

`.claude/settings.json`:
```json
{
  "env": {
    "TELEGRAM_STATE_DIR": "/home/feihong/code/<ProjectName>/.telegram"
  },
  "enabledPlugins": {
    "telegram@claude-plugins-official": true
  }
}
```

Telegram 상태 파일 위치: `/home/feihong/code/<ProjectName>/.telegram/`

---

### Docker Claude 컨테이너 (node)

컨테이너 내부의 프로젝트 `.claude/settings.json`:
```json
{
  "env": {
    "TELEGRAM_STATE_DIR": "/home/node/<workspace>/.telegram"
  },
  "enabledPlugins": {
    "telegram@claude-plugins-official": true
  }
}
```

Telegram 상태 파일 위치: 컨테이너 내부 `/home/node/<workspace>/.telegram/`

---

## 주의사항

### 1. TELEGRAM_STATE_DIR 미설정 시 위험

`TELEGRAM_STATE_DIR`을 설정하지 않으면 `/telegram:configure` 실행 시
각 계정의 **전역 경로**(`~/.claude/channels/telegram/`)에 저장됨.

- Host에서 실행 → `/home/feihong/.claude/channels/telegram/` 저장
- Docker에서 실행 → `/home/node/.claude/channels/telegram/` 저장

bind mount 설정에 따라 두 경로가 같은 파일을 가리킬 수 있으므로 반드시 프로젝트 로컬로 분리할 것.

### 2. 전역 경로 점검

문제가 생기면 아래 경로에 불필요한 파일이 없는지 확인:
```bash
# Host
ls ~/.claude/channels/telegram/

# Docker 컨테이너 내부
docker exec <container> ls /home/node/.claude/channels/telegram/
```

전역 경로에 파일이 있다면, 어느 프로젝트의 것인지 확인 후 해당 프로젝트 로컬로 이동하고 전역 경로에서 삭제.

### 3. docker-compose.yml 볼륨 설계 원칙

- `.credentials.json` bind mount는 **파일 단위**로만 마운트
- `.claude/` 디렉토리 전체를 named volume으로 마운트하면 Telegram 설정 경로가 꼬일 수 있음
- Telegram 설정은 프로젝트 workspace 경로에 별도 관리 권장

---

## 설정 검증 체크리스트

- [ ] Host 프로젝트 `.claude/settings.json`에 `TELEGRAM_STATE_DIR` 절대경로 설정
- [ ] Docker Claude 프로젝트 `.claude/settings.json`에 `TELEGRAM_STATE_DIR` 절대경로 설정
- [ ] Host `~/.claude/channels/telegram/` 전역 경로가 비어 있음
- [ ] Docker 컨테이너 내 `/home/node/.claude/channels/telegram/` 전역 경로가 비어 있음
- [ ] `.gitignore`에 `.telegram/` 포함
