# Telegram Plugin 프로젝트별 설정 가이드

## 문제

Claude Code의 Telegram 플러그인은 기본적으로 토큰을 전역 경로에 저장합니다:

```
~/.claude/channels/telegram/.env
```

여러 프로젝트에서 서로 다른 Telegram 봇을 사용할 경우, `/telegram:configure`를 실행할 때마다 기존 토큰이 덮어쓰여져서 다른 프로젝트의 봇이 동작하지 않게 됩니다.

## 해결 방법

`TELEGRAM_STATE_DIR` 환경변수를 프로젝트별로 다르게 설정하면 토큰과 access 설정이 프로젝트 단위로 분리됩니다.

### 1단계: 프로젝트 settings.json에 환경변수 추가

`.claude/settings.json` 파일에 `env` 항목을 추가합니다:

```json
{
  "env": {
    "TELEGRAM_STATE_DIR": "/absolute/path/to/project/.telegram",
    "PATH": "/home/feihong/.bun/bin:${PATH}"
  },
  "enabledPlugins": {
    "telegram@claude-plugins-official": true
  }
}
```

> `TELEGRAM_STATE_DIR`은 반드시 **절대 경로**로 지정해야 합니다.

### 2단계: Telegram 봇 설정

프로젝트 디렉토리에서 Claude Code를 실행한 뒤 `/telegram:configure` 스킬을 실행합니다. 봇 토큰이 프로젝트의 `.telegram/.env` 파일에 저장됩니다.

### 3단계: .gitignore에 추가

`.telegram/` 디렉토리에는 봇 토큰과 접근 권한 정보가 포함되므로 반드시 `.gitignore`에 추가합니다:

```
.telegram/
```

## 디렉토리 구조

설정 완료 후 프로젝트에 생성되는 파일:

```
project/
├── .claude/
│   └── settings.json      # TELEGRAM_STATE_DIR 환경변수 포함
├── .telegram/
│   ├── .env               # TELEGRAM_BOT_TOKEN=...
│   ├── access.json        # 접근 허용 정책
│   └── approved/          # 승인된 페어링 정보
└── .gitignore             # .telegram/ 포함
```

## 주의: 페어링 승인 시 경로 문제

### 원인

`/telegram:access` 스킬의 SKILL.md(`~/.claude/plugins/cache/claude-plugins-official/telegram/0.0.4/skills/access/SKILL.md`)는 `access.json` 경로를 `~/.claude/channels/telegram/`으로 하드코딩하고 있습니다. 반면, 서버(`server.ts`)는 `TELEGRAM_STATE_DIR` 환경변수를 인식하여 프로젝트 로컬 경로를 사용합니다.

따라서 `TELEGRAM_STATE_DIR`을 프로젝트 로컬로 설정한 경우:
- **서버**: pending 항목을 프로젝트의 `.telegram/access.json`에 저장 (정상)
- **`/telegram:access pair` 스킬**: 전역 경로 `~/.claude/channels/telegram/access.json`에서 코드를 찾으려 함 (실패)

### 해결: 수동 승인 절차

Claude에게 프로젝트 로컬 `access.json`을 직접 수정하도록 요청합니다:

1. `.telegram/access.json`을 읽어서 `pending`에 있는 페어링 코드 확인
2. 해당 `senderId`를 `allowFrom` 배열에 추가
3. `pending`에서 해당 코드 삭제
4. `.telegram/approved/<senderId>` 파일 생성 (내용: chatId)

Claude에게 요청하는 방법:

```
.telegram/access.json 파일을 읽고 페어링 코드 <code>를 승인해 줘
```

## Claude에게 지시하는 방법

새 프로젝트에서 Telegram 봇을 설정할 때 Claude에게 다음과 같이 요청하면 됩니다:

```
이 프로젝트에 Telegram 플러그인을 설정해 줘.
TELEGRAM_STATE_DIR을 프로젝트 로컬(.telegram/)로 분리해서 설정해 줘.
```

Claude가 수행할 작업:
1. `.claude/settings.json`에 `env.TELEGRAM_STATE_DIR` 추가 (절대 경로)
2. `.gitignore`에 `.telegram/` 추가
3. `/telegram:configure` 실행하여 봇 토큰 설정
