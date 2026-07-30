#!/usr/bin/env bash
#
# Jarvis 문서 사이트(mkdocs_claude) 자동 반영 watcher.
#
# 왜 필요한가:
#   mkdocs serve 의 자체 파일 감시가 이 환경(호스트 → 컨테이너 bind mount)에서
#   동작하지 않는다. 그래서 호스트에서 폴링해 변화가 있으면 컨테이너를 재시작한다.
#
# 왜 여기(MakeSQL/scripts)에 있는가:
#   원래 /home/feihong/code/DockerClaude/Mkdocs/watch.sh 에 있었는데, 2026-07-28
#   디렉토리 재편 때 그 경로가 통째로 사라졌다. systemd 유닛은 그대로 남아
#   3초마다 203/EXEC 로 재시도만 반복했고, 사이트는 사흘간 Jul 28 빌드에 멈춰
#   있었다 — 그 뒤 추가된 문서는 전부 404 였는데 아무도 알아채지 못했다.
#   git 으로 추적되는 저장소에 두면 같은 방식으로 사라지지 않는다.
#   유닛이 보는 경로는 이 파일로 향하는 심볼릭 링크다.
#
# 유닛: /etc/systemd/system/mkdocs-watch.service (User=feihong)
#   ExecStart=/home/feihong/code/DockerClaude/Mkdocs/watch.sh → 이 파일

set -uo pipefail

DOCS_DIR=${DOCS_DIR:-/home/feihong/code/Jarvis}
CONTAINER=${CONTAINER:-mkdocs_claude}
INTERVAL=${INTERVAL:-5} # 폴링 주기(초)
DEBOUNCE=${DEBOUNCE:-5} # 마지막 변경 후 이만큼 조용해야 재시작

log() { echo "$(date '+%F %T') [mkdocs-watch] $*"; }

# 문서 트리의 상태를 한 줄로 요약한다. 경로와 mtime 을 함께 넣어 파일 추가·삭제·
# 수정이 모두 잡힌다. 648개 기준 50ms 정도라 5초 폴링에 부담이 없다.
snapshot() {
	find "$DOCS_DIR" \( -name '*.md' -o -name '*.yml' \) \
		-not -path '*/.git/*' -printf '%p %T@\n' 2>/dev/null | sort | md5sum
}

if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
	log "경고: 컨테이너 $CONTAINER 를 찾을 수 없다. 계속 감시는 하되 재시작은 실패한다."
fi

log "시작 — $DOCS_DIR → $CONTAINER (폴링 ${INTERVAL}s, 디바운스 ${DEBOUNCE}s)"
prev=$(snapshot)

while true; do
	sleep "$INTERVAL"
	cur=$(snapshot)
	[ "$cur" = "$prev" ] && continue

	# 연속 편집(에디터 저장, 스크립트 일괄 생성) 중에 매번 재시작하지 않도록
	# 조용해질 때까지 기다린다. 재빌드가 80초쯤 걸려서 겹치면 손해가 크다.
	log "변경 감지 — 안정될 때까지 대기"
	while true; do
		sleep "$DEBOUNCE"
		settled=$(snapshot)
		[ "$settled" = "$cur" ] && break
		cur=$settled
	done

	log "재시작: $CONTAINER"
	if docker restart "$CONTAINER" >/dev/null 2>&1; then
		log "재시작 완료 (재빌드에 1분 이상 걸린다)"
	else
		log "재시작 실패 — 다음 주기에 다시 시도"
	fi
	prev=$(snapshot)
done
