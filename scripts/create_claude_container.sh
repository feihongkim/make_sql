#!/bin/bash
#
# claude 컨테이너 생성기 — 런북 3절 + 10-4절을 하나로.
# 런북: ~/code/Jarvis/project/MakeSQL/claude_docker_runbook.md
#
# 사용법:
#   ./scripts/create_claude_container.sh <PROJECT> [USER_ID]
#
#   PROJECT   ~/code 아래 폴더명, 또는 절대경로 (예: apply, /data2/rstudio)
#   USER_ID   텔레그램 user id. 생략하면 텔레그램 없이(agentapi 채널만) 구성한다.
#
# 예시:
#   ./scripts/create_claude_container.sh apply 7723743534
#   ./scripts/create_claude_container.sh myproj              # 텔레그램 없이
#
# 환경변수:
#   CLAUDE_VERSION   설치할 Claude Code 버전 (기본 아래 값)
#                    확인: npm view @anthropic-ai/claude-code version
#
# 이 스크립트는 여러 번 돌려도 안전하다. 매번 완전한 세트를 생성하므로
# agentapi 배선이 빠지거나 덮어써지는 일이 없다.
#
# 워크스페이스에 두면 동작이 바뀌는 파일 (둘 다 사람이 만든다):
#   tele.token   봇 토큰 한 줄. 있으면 텔레그램 채널을 켠다. USER_ID 가 함께 필요하다.
#   mount-ssh    빈 파일이어도 된다. 있으면 호스트의 ~/.ssh 를 읽기 전용으로 붙인다.
#                (touch mount-ssh)
#
# ⚠️ 생성하지 않는 것: tele.token. 봇은 사람이 @BotFather 에서 만들어야 한다.
#    컨테이너마다 새 봇이 필요하다 — 같은 토큰을 둘이 물면 409 Conflict 로 하나가 죽는다.

set -euo pipefail

# 아래에서 워크스페이스로 cd 하므로, 이 스크립트가 있는 곳은 지금 잡아둔다.
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

CLAUDE_VERSION=${CLAUDE_VERSION:-2.1.220}
CODE_ROOT=${CODE_ROOT:-/home/feihong/code}
HOST_USER=${HOST_USER:-feihong}

PROJECT_ARG=${1:-}
USER_ID=${2:-}

if [ -z "$PROJECT_ARG" ]; then
  sed -n '3,22p' "$0" | sed 's/^# \{0,1\}//'
  exit 1
fi

# --- 경로 해석 --------------------------------------------------------------
if [[ "$PROJECT_ARG" == /* ]]; then
  WORKSPACE="$PROJECT_ARG"
else
  WORKSPACE="$CODE_ROOT/$PROJECT_ARG"
fi
PROJECT=$(basename "$WORKSPACE")
CONTAINER="${PROJECT}_claude"

if [ ! -d "$WORKSPACE" ]; then
  echo "오류: 프로젝트 디렉토리가 없습니다 — $WORKSPACE" >&2
  exit 1
fi

# --- 사전 점검 --------------------------------------------------------------
# agentapi 플러그인이 호스트에 설치돼 있지 않으면 컨테이너가 떠도 채널이 등록되지 않는다.
MARKET_DIR="/home/$HOST_USER/.claude/marketplaces-local/makesql-channels"
if [ ! -f "$MARKET_DIR/.claude-plugin/marketplace.json" ]; then
  echo "⚠️  agentapi 플러그인이 없습니다 — $MARKET_DIR" >&2
  echo "    먼저 설치하세요 (런북 10-3):" >&2
  echo "      git clone git@github.com:feihongkim/claude_extension.git $MARKET_DIR" >&2
  echo "      claude plugin marketplace add $MARKET_DIR" >&2
  echo "      claude plugin install agentapi@makesql-channels" >&2
  exit 1
fi

cd "$WORKSPACE"

# --- 봇 토큰 읽기 -----------------------------------------------------------
BOT_TOKEN=""
[ -f tele.token ] && BOT_TOKEN=$(tr -d ' \r\n' < tele.token)

if [ -n "$BOT_TOKEN" ] && [ -z "$USER_ID" ]; then
  echo "오류: tele.token 이 있는데 USER_ID 가 없습니다." >&2
  echo "      허용목록에 넣을 텔레그램 user id 를 두 번째 인자로 주세요." >&2
  exit 1
fi

if [ -n "$BOT_TOKEN" ]; then
  echo "▶ $CONTAINER — 텔레그램 + agentapi 채널로 구성"
else
  echo "▶ $CONTAINER — agentapi 채널만으로 구성 (tele.token 없음)"
fi

# --- SSH 키 마운트 여부 -----------------------------------------------------
# 워크스페이스에 mount-ssh 파일이 있으면 호스트의 ~/.ssh 를 읽기 전용으로 붙인다.
#
# 플래그가 아니라 표시 파일인 이유: 이 스크립트는 매번 완전한 세트를 새로
# 생성하므로, 플래그로 두면 재실행할 때 빠뜨리는 순간 마운트가 조용히 사라진다.
# 워크스페이스에 남는 파일이어야 재생성해도 같은 결과가 나온다 (tele.token 과 같은 방식).
MOUNT_SSH=""
[ -e mount-ssh ] && MOUNT_SSH=1

if [ -n "$MOUNT_SSH" ]; then
  echo "  · SSH 키 마운트: /home/$HOST_USER/.ssh (읽기 전용)"
  echo "    ⚠️ 이 컨테이너의 에이전트가 호스트의 SSH 키 전체를 쓸 수 있게 된다."
fi

# --- 문서 디렉토리 (/docs) --------------------------------------------------
# 이 프로젝트의 문서는 Jarvis/project/<프로젝트> 에 있다. pi 컨테이너는 예전부터
# 그것을 /docs 로 붙여 왔는데 claude 컨테이너에는 빠져 있었다. 그래서 워크스페이스
# CLAUDE.md 가 /docs/... 를 가리켜도 에이전트가 읽지 못했다 (LS_claude 가 그랬다).
#
# 표시 파일이 필요 없다 — 경로가 있으면 붙이고 없으면 넘어가는 규칙이라 자동으로 판단한다.
DOCS_DIR="$CODE_ROOT/Jarvis/project/$PROJECT"
[ -d "$DOCS_DIR" ] || DOCS_DIR=""

if [ -n "$DOCS_DIR" ]; then
  echo "  · 문서 마운트: $DOCS_DIR → /docs"
else
  echo "  · 문서 마운트 없음 ($CODE_ROOT/Jarvis/project/$PROJECT 가 없다)"
fi

# --- 디렉토리 ---------------------------------------------------------------
# .claude-state 하위를 미리 만든다. 없으면 Docker 가 root 소유로 생성해 컨테이너가 못 쓴다.
mkdir -p docker-config/telegram
mkdir -p .claude-state/{channels,projects,sessions,shell-snapshots,file-history,debug,ide,session-env}

# --- 봇 토큰 / 허용목록 -----------------------------------------------------
# 토큰이 없어도 빈 파일을 만든다. Dockerfile COPY 대상이라 파일 자체는 있어야 한다.
printf 'TELEGRAM_BOT_TOKEN=%s\n' "$BOT_TOKEN" > docker-config/telegram/.env
chmod 600 docker-config/telegram/.env

cat > docker-config/telegram/access.json <<EOF
{
  "dmPolicy": "allowlist",
  "allowFrom": ["${USER_ID:-0}"],
  "groups": {},
  "pending": {}
}
EOF

# --- Claude 설정 3벌 --------------------------------------------------------
# entrypoint 가 토큰 유무를 보고 앞의 둘 중 하나를 고른다.
# 세 번째는 ./abledb ask --new 전용이다.

cat > docker-config/claude-settings.json <<'EOF'
{
  "model": "sonnet",
  "enabledPlugins": {
    "telegram@claude-plugins-official": true,
    "agentapi@makesql-channels": true
  },
  "skipDangerousModePermissionPrompt": true,
  "env": {
    "DISABLE_AUTOUPDATER": "1"
  }
}
EOF

# 텔레그램 없이 기동할 때. false 를 명시해야 한다 — 생략하면 전역 설치된 플러그인이
# 그대로 활성이라 --channels 를 안 줘도 MCP 서버가 떠서 봇 토큰을 물어버린다.
cat > docker-config/claude-settings-notelegram.json <<'EOF'
{
  "model": "sonnet",
  "enabledPlugins": {
    "telegram@claude-plugins-official": false,
    "agentapi@makesql-channels": true
  },
  "skipDangerousModePermissionPrompt": true,
  "env": {
    "DISABLE_AUTOUPDATER": "1"
  }
}
EOF

# 일회성 실행 전용. 두 플러그인을 모두 끈다.
# settings-notelegram.json 을 재사용하면 안 된다 — agentapi 가 활성이라 일회성
# 프로세스가 채널 포트(8799)를 뺏으려다 실패하고, 그 실패가 호스트 공유 캐시
# (~/.claude/mcp-needs-auth-cache.json)에 기록돼 전 컨테이너의 채널이 죽는다.
cat > docker-config/claude-settings-oneshot.json <<'EOF'
{
  "model": "sonnet",
  "enabledPlugins": {
    "telegram@claude-plugins-official": false,
    "agentapi@makesql-channels": false
  },
  "skipDangerousModePermissionPrompt": true,
  "env": {
    "DISABLE_AUTOUPDATER": "1"
  }
}
EOF

# --- 채널 정책 (policy tier) ------------------------------------------------
# agentapi 는 Anthropic 기본 allowlist(tengu_harbor_ledger)에 없어서 이것이 없으면
# 채널이 조용히 등록되지 않는다.
# ⚠️ allowedChannelPlugins 는 기본 목록을 "대체"한다. telegram 을 빼면 텔레그램 봇까지
#    죽고, channelsEnabled 를 빼면 전 채널이 정책 차단된다.
cat > docker-config/managed-settings.json <<'EOF'
{
  "channelsEnabled": true,
  "allowedChannelPlugins": [
    { "marketplace": "claude-plugins-official", "plugin": "telegram" },
    { "marketplace": "makesql-channels", "plugin": "agentapi" }
  ]
}
EOF

# --- 앱 상태: 온보딩·폴더신뢰 프롬프트 스킵 ---------------------------------
# hasTrustDialogAccepted 가 없으면 컨테이너가 기동 직후 신뢰 확인 프롬프트에서 멈춘다.
cat > docker-config/claude.json <<'EOF'
{
  "hasCompletedOnboarding": true,
  "projects": {
    "/workspace": {
      "hasTrustDialogAccepted": true,
      "projectOnboardingSeenCount": 1,
      "hasClaudeMdExternalIncludesApproved": true,
      "hasClaudeMdExternalIncludesWarningShown": true
    }
  }
}
EOF

# --- entrypoint -------------------------------------------------------------
cat > docker-config/entrypoint.sh <<'EOF'
#!/bin/bash
set -e

# 봇 토큰이 실제로 들어 있는지 확인한다.
# 없거나 비어 있으면 텔레그램 없이(agentapi 채널만) 기동한다.
TOKEN_VALUE=""
if [ -f "$HOME/.defaults/telegram/.env" ]; then
  TOKEN_VALUE=$(sed -n 's/^TELEGRAM_BOT_TOKEN=//p' "$HOME/.defaults/telegram/.env" | head -1 | tr -d ' \r\n')
fi

if [ -n "$TOKEN_VALUE" ]; then
  # channels/ 는 프로젝트 전용 마운트이므로, 플러그인 기본 경로에 그대로 넣으면 된다.
  TG_DIR="$HOME/.claude/channels/telegram"
  mkdir -p "$TG_DIR/approved"

  if [ ! -f "$TG_DIR/.env" ]; then
    cp "$HOME/.defaults/telegram/.env" "$TG_DIR/.env"
    echo "[entrypoint] 봇 토큰 초기화"
  fi
  if [ ! -f "$TG_DIR/access.json" ]; then
    cp "$HOME/.defaults/telegram/access.json" "$TG_DIR/access.json"
    echo "[entrypoint] access.json 초기화"
  fi
  chmod 600 "$TG_DIR/.env" "$TG_DIR/access.json"

  echo "[entrypoint] 텔레그램 채널 활성화"
  # settings.json 을 $HOME/.claude 에 쓰면 호스트 전역 설정을 덮어쓴다. --settings 로 넘긴다.
  exec claude \
    --dangerously-skip-permissions \
    --settings "$HOME/.defaults/settings.json" \
    --channels plugin:telegram@claude-plugins-official plugin:agentapi@makesql-channels
else
  echo "[entrypoint] 봇 토큰 없음 → agentapi 채널만 활성화"
  exec claude \
    --dangerously-skip-permissions \
    --settings "$HOME/.defaults/settings-notelegram.json" \
    --channels plugin:agentapi@makesql-channels
fi
EOF

# --- Dockerfile -------------------------------------------------------------
cat > Dockerfile <<EOF
FROM node:20-slim

RUN apt-get update && apt-get install -y \\
    openssh-client bash curl unzip git procps \\
    && rm -rf /var/lib/apt/lists/*

# 컨테이너 사용자 홈을 호스트와 동일한 경로로 맞춘다.
# ~/.claude 안의 JSON(known_marketplaces.json, installed_plugins.json)이 절대경로를
# 저장하므로, 홈 경로가 다르면 공유된 plugins/ 가 통째로 로드에 실패한다.
RUN usermod -l $HOST_USER -d /home/$HOST_USER -m node && groupmod -n $HOST_USER node

# bun (telegram / agentapi 채널 서버가 bun 으로 실행됨)
RUN curl -fsSL https://bun.sh/install | BUN_INSTALL=/usr/local bash

# 버전을 명시한다. @latest 로 두면 Docker 레이어 캐시 때문에 첫 빌드 시점 버전에
# 계속 묶인다 (재빌드해도 안 올라간다). 올릴 때 이 숫자를 바꾼다.
ARG CLAUDE_VERSION=$CLAUDE_VERSION
RUN npm install -g @anthropic-ai/claude-code@\${CLAUDE_VERSION}

# 컨테이너 전용 기본값 — .claude 밖이라 볼륨 마운트에 덮이지 않는다
RUN mkdir -p /home/$HOST_USER/.defaults/telegram
COPY docker-config/claude-settings.json            /home/$HOST_USER/.defaults/settings.json
COPY docker-config/claude-settings-notelegram.json /home/$HOST_USER/.defaults/settings-notelegram.json
# 일회성 실행(./abledb ask --new) 전용. 두 플러그인을 모두 끈다.
# settings-notelegram.json 을 재사용하면 agentapi 가 활성이라 채널 포트를 뺏으려다
# 실패하고, 그 실패가 호스트 공유 캐시에 기록돼 전 컨테이너의 채널이 죽는다. (런북 10-6)
COPY docker-config/claude-settings-oneshot.json    /home/$HOST_USER/.defaults/settings-oneshot.json
COPY docker-config/telegram/.env                   /home/$HOST_USER/.defaults/telegram/.env
COPY docker-config/telegram/access.json            /home/$HOST_USER/.defaults/telegram/access.json

# 채널 정책 (policy tier). agentapi 는 Anthropic 기본 allowlist(tengu_harbor_ledger)에
# 없으므로 여기서 승인한다. 주의: allowedChannelPlugins 는 기본 목록을 "대체"하므로
# telegram 도 반드시 함께 적어야 하고, channelsEnabled 를 빼면 전 채널이 차단된다.
RUN mkdir -p /etc/claude-code
COPY docker-config/managed-settings.json /etc/claude-code/managed-settings.json

# 앱 상태. 마운트하지 않고 이미지에 굽는다
COPY docker-config/claude.json /home/$HOST_USER/.claude.json

RUN chown -R $HOST_USER:$HOST_USER /home/$HOST_USER/.defaults /home/$HOST_USER/.claude.json

COPY docker-config/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /workspace
USER $HOST_USER
CMD ["/entrypoint.sh"]
EOF

# --- docker-compose.yml -----------------------------------------------------
{
  cat <<EOF
services:
  claude:
    build: .
    container_name: $CONTAINER
    restart: unless-stopped
    stdin_open: true
    tty: true
    volumes:
      # 작업 대상 소스
      - $WORKSPACE:/workspace

      # 전역 공유 (베이스) — 자격증명, 플러그인
      # 컨테이너 홈 경로를 호스트와 동일하게 맞춰야 plugins/ 안의 절대경로가 해석된다
      - /home/$HOST_USER/.claude:/home/$HOST_USER/.claude
EOF
  if [ -n "$DOCS_DIR" ]; then
    cat <<EOF

      # 이 프로젝트의 문서 (Jarvis/project/$PROJECT). pi 컨테이너와 같은 자리에 붙인다.
      # 워크스페이스 CLAUDE.md 가 /docs/... 를 참조하므로, 없으면 에이전트가
      # 자기 지침이 가리키는 문서를 읽지 못한다.
      # 쓰기 가능 — 문서를 갱신하는 것이 정상 작업이다.
      - $DOCS_DIR:/docs
EOF
  fi
  if [ -n "$MOUNT_SSH" ]; then
    cat <<EOF

      # SSH 키 (읽기 전용) — 워크스페이스에 mount-ssh 파일이 있어서 붙였다.
      # ⚠️ 이 컨테이너의 에이전트가 호스트의 SSH 키 전체를 쓸 수 있다.
      #
      # known_hosts 도 읽기 전용이라 **처음 보는 호스트에는 접속하지 못한다**
      # ("Host key verification failed"). 호스트에서 한 번 ssh 해서 등록하면
      # 같은 파일을 보므로 컨테이너에서도 바로 된다.
      - /home/$HOST_USER/.ssh:/home/$HOST_USER/.ssh:ro
EOF
  fi
  cat <<EOF

      # 프로젝트 전용 — 위 베이스의 하위 경로를 덮어쓴다
EOF
  for d in channels projects sessions shell-snapshots file-history debug ide session-env; do
    echo "      - $WORKSPACE/.claude-state/$d:/home/$HOST_USER/.claude/$d"
  done
  cat <<'EOF'

    # pi 계층과 같은 네트워크에 둔다. 컨테이너 이름으로 서로 부를 수 있어야
    # 공용 서비스(차트 렌더러 등)를 claude·pi 양쪽에서 함께 쓸 수 있다.
    # 이게 없으면 별도 compose 네트워크에 갇혀 이름 해석이 안 된다.
    networks:
      - pinet

networks:
  pinet:
    external: true
EOF
} > docker-compose.yml

# --- .gitignore -------------------------------------------------------------
# 이미 있으면 중복 추가하지 않는다.
touch .gitignore
# mount-ssh 는 일부러 넣지 않는다. 비밀값이 아니고, 무시해 버리면 다른 데서
# 재생성할 때 SSH 마운트가 조용히 빠진다 — 이 표시 파일이 막으려던 바로 그 상황이다.
for pat in ".claude-state/" "docker-config/telegram/" "docker-config/ssh/" "tele.token" ".claude/"; do
  grep -qxF "$pat" .gitignore || echo "$pat" >> .gitignore
done

echo
echo "생성 완료: $WORKSPACE${MOUNT_SSH:+  (SSH 키 마운트 포함)}"
echo "  Dockerfile, docker-compose.yml, .gitignore"
echo "  docker-config/{entrypoint.sh,claude.json,managed-settings.json}"
echo "  docker-config/claude-settings{,-notelegram,-oneshot}.json"
echo "  docker-config/telegram/{.env,access.json}"
echo
echo "다음:"
echo "  cd $WORKSPACE && docker compose up -d --build"
echo ""
echo "  # ⚠️ 기동 후 반드시 채널을 확인한다. recreate 하면 채널이 없는 채로 뜨는 일이 있고"
echo "  #    (mcp-needs-auth-cache.json 오염), 컨테이너는 정상으로 보여서 알아채기 어렵다."
echo "  #    아래 스크립트가 확인하고 필요하면 캐시를 지운 뒤 재시작까지 한다."
echo "  $SCRIPT_DIR/check_claude_channels.sh $CONTAINER"
echo ""
echo "  ./abledb ask $CONTAINER \"안녕\""
