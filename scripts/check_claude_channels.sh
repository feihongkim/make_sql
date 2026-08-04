#!/usr/bin/env bash
#
# claude 컨테이너의 채널이 제대로 떴는지 확인하고, 안 떴으면 복구한다.
#
#   ./scripts/check_claude_channels.sh                 떠 있는 *_claude 전부
#   ./scripts/check_claude_channels.sh LS_claude       하나만
#   NO_FIX=1 ./scripts/check_claude_channels.sh        확인만 하고 고치지 않음
#
# 왜 필요한가:
#   컨테이너를 recreate/restart 하면 채널 플러그인 기동 실패가
#   ~/.claude/mcp-needs-auth-cache.json 에 기록되는 일이 있다. 이 파일은 호스트
#   공유라, 한 번 올라가면 그 뒤로 **모든 claude 컨테이너가 재시작마다 채널 없이 뜬다.**
#   컨테이너는 정상으로 보이고 로그도 조용해서 알아채기 어렵다 — 텔레그램에 말을
#   걸어봐야 그제야 안다. (2026-08-02 사고, 2026-08-04 재발)
#
#   캐시를 지우고 재시작하면 20초 안에 복구된다. 캐시라 지워도 안전하다.

set -uo pipefail

CACHE="$HOME/.claude/mcp-needs-auth-cache.json"
WAIT_SECS=${WAIT_SECS:-60}   # 채널이 뜰 때까지 기다리는 시간
NO_FIX=${NO_FIX:-}

# 이름이 _claude 로 끝나도 에이전트 컨테이너가 아닌 것이 있다 (mkdocs_claude 는
# mkdocs-material 사이트다). claude 바이너리 유무로 가른다.
is_claude_agent() {
	docker exec "$1" sh -c 'command -v claude' >/dev/null 2>&1
}

targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
	mapfile -t candidates < <(docker ps --format '{{.Names}}' | grep -E '_claude$' | sort)
	targets=()
	for c in "${candidates[@]}"; do
		is_claude_agent "$c" && targets+=("$c")
	done
fi

if [ ${#targets[@]} -eq 0 ]; then
	echo "떠 있는 *_claude 컨테이너가 없다." >&2
	exit 1
fi

# 이 컨테이너가 텔레그램 모드인지 본다. 토큰이 비어 있으면 agentapi 하나만 떠야 정상이다.
expected_channels() {
	local c=$1 tok
	tok=$(docker exec "$c" sh -c \
		'sed -n "s/^TELEGRAM_BOT_TOKEN=//p" $HOME/.defaults/telegram/.env 2>/dev/null | tr -d " \r\n"' 2>/dev/null)
	[ -n "$tok" ] && echo 2 || echo 1
}

running_channels() {
	docker exec "$1" sh -c 'ps -eo args | grep -c "[b]un run"' 2>/dev/null | tr -d ' \n'
}

# 채널이 want 개가 될 때까지 기다린다. 되면 0, 시간 초과면 1.
wait_for_channels() {
	local c=$1 want=$2 waited=0 n
	while [ "$waited" -lt "$WAIT_SECS" ]; do
		n=$(running_channels "$c")
		[ "${n:-0}" -ge "$want" ] 2>/dev/null && return 0
		sleep 5
		waited=$((waited + 5))
	done
	return 1
}

cache_has_channels() {
	[ -f "$CACHE" ] && grep -q 'plugin:' "$CACHE" 2>/dev/null
}

fail=0
fixed=0

for c in "${targets[@]}"; do
	if ! docker inspect "$c" >/dev/null 2>&1; then
		echo "❌ $c — 그런 컨테이너가 없다"
		fail=1
		continue
	fi

	want=$(expected_channels "$c")
	mode=$([ "$want" = 2 ] && echo "텔레그램+agentapi" || echo "agentapi 만")

	if wait_for_channels "$c" "$want"; then
		echo "✅ $c — 채널 $(running_channels "$c")/$want ($mode)"
		continue
	fi

	echo "⚠️  $c — 채널 $(running_channels "$c")/$want ($mode) — 기동 안 됨"

	if [ -n "$NO_FIX" ]; then
		echo "   NO_FIX 지정됨 — 고치지 않는다"
		fail=1
		continue
	fi

	if ! cache_has_channels; then
		echo "   auth 캐시는 깨끗하다 ($CACHE). 다른 원인이다."
		echo "   확인: docker exec $c claude plugin list"
		echo "         ~/code/<프로젝트>/.claude/settings*.json 의 TELEGRAM_STATE_DIR 잔재"
		fail=1
		continue
	fi

	echo "   auth 캐시 오염 확인 → 백업 후 제거하고 재시작한다"
	cp -p "$CACHE" "$CACHE.bak.$(date +%Y%m%d%H%M%S)" 2>/dev/null
	rm -f "$CACHE"
	docker restart "$c" >/dev/null

	if wait_for_channels "$c" "$want"; then
		echo "   ✅ 복구됨 — 채널 $(running_channels "$c")/$want"
		fixed=$((fixed + 1))
	else
		echo "   ❌ 복구 실패 — docker logs $c 를 확인하라"
		fail=1
	fi
done

if [ "$fixed" -gt 0 ]; then
	echo
	echo "※ auth 캐시는 호스트 공유다. 다른 claude 컨테이너도 다음 재시작 때"
	echo "  같은 증상이 났을 것이므로, 인자 없이 한 번 더 돌려 전체를 점검해 두면 좋다."
fi

exit $fail
