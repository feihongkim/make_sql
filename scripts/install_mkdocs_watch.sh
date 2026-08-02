#!/usr/bin/env bash
#
# mkdocs-watch.service 가 MakeSQL/scripts/mkdocs_watch.sh 를 실행하도록 고친다.
#
#   sudo bash scripts/install_mkdocs_watch.sh
#
# 원래 유닛은 /home/feihong/code/DockerClaude/Mkdocs/watch.sh 를 가리켰는데,
# 그 디렉토리가 2026-07-28 재편 때 사라져 3초마다 203/EXEC 로 재시도만 반복했다.
# 그동안 Jarvis 문서 사이트는 Jul 28 빌드에 멈춰 있었고 새 문서는 전부 404 였다.
#
# 원본 유닛 파일은 건드리지 않고 drop-in 으로 덮어쓴다. 되돌리려면
# /etc/systemd/system/mkdocs-watch.service.d/ 를 지우고 daemon-reload 하면 된다.
# 여러 번 실행해도 안전하다.

set -euo pipefail

UNIT=mkdocs-watch.service
TARGET=/home/feihong/code/MakeSQL/scripts/mkdocs_watch.sh
DROPIN_DIR=/etc/systemd/system/${UNIT}.d

if [ "$(id -u)" -ne 0 ]; then
	echo "오류: root 로 실행해야 한다 — sudo bash $0" >&2
	exit 1
fi

if [ ! -x "$TARGET" ]; then
	echo "오류: $TARGET 가 없거나 실행 권한이 없다." >&2
	echo "      chmod +x $TARGET 후 다시 실행하라." >&2
	exit 1
fi

if ! systemctl cat "$UNIT" >/dev/null 2>&1; then
	echo "오류: $UNIT 유닛이 등록돼 있지 않다." >&2
	exit 1
fi

echo "[1/3] drop-in 작성: $DROPIN_DIR/override.conf"
mkdir -p "$DROPIN_DIR"
# 빈 ExecStart= 로 원래 값을 지운 뒤 새 경로를 넣는다 (systemd 규칙).
cat >"$DROPIN_DIR/override.conf" <<EOF
[Service]
ExecStart=
ExecStart=$TARGET
EOF

echo "[2/3] daemon-reload + 재시작"
systemctl daemon-reload
systemctl restart "$UNIT"

echo "[3/3] 상태 확인 (3초 대기)"
sleep 3
systemctl status "$UNIT" --no-pager | head -8

echo
if systemctl is-active --quiet "$UNIT"; then
	echo "✅ watcher 가동 중. 이제 Jarvis 문서를 고치면 5~10초 뒤 컨테이너가"
	echo "   재시작되고, 재빌드에 1분 남짓 걸린 뒤 사이트에 반영된다."
	echo "   로그: journalctl -u $UNIT -f"
else
	echo "❌ 아직 활성이 아니다. 위 상태 출력과 journalctl -u $UNIT -n 30 을 확인하라."
	exit 1
fi
