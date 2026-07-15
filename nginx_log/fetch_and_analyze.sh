#!/bin/bash
# nginx 로그를 원격 서버에서 가져와 보안 분석 후 JSON 출력
# 사용법: ./fetch_and_analyze.sh [시간수(기본2)]

HOURS=${1:-2}
SSH_KEY="$HOME/moodle.pem"
REMOTE_HOST="ubuntu@3.34.223.162"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TMP_ACCESS="/tmp/nginx_access_$$.log"
TMP_ERROR="/tmp/nginx_error_$$.log"
TMP_F2B="/tmp/nginx_fail2ban_$$.log"
TMP_BLOCKLIST="/tmp/nginx_blocklist_$$.conf"

cleanup() {
    rm -f "$TMP_ACCESS" "$TMP_ERROR" "$TMP_F2B"
    rm -f "$TMP_BLOCKLIST"
}
trap cleanup EXIT

SSH_CMD="ssh -i $SSH_KEY -o ConnectTimeout=10 -o StrictHostKeyChecking=no $REMOTE_HOST"

# access.log + access.log.1 합쳐서 가져오기 (로그 로테이션 대응)
$SSH_CMD "cat /var/log/nginx/access.log.1 /var/log/nginx/access.log 2>/dev/null" \
    > "$TMP_ACCESS" 2>/dev/null

# error.log + error.log.1
$SSH_CMD "cat /var/log/nginx/error.log.1 /var/log/nginx/error.log 2>/dev/null" \
    > "$TMP_ERROR" 2>/dev/null

# fail2ban 로그

# geo_blocklist 가져오기
$SSH_CMD "cat /etc/nginx/conf.d/geo_blocklist.conf 2>/dev/null" > "$TMP_BLOCKLIST" 2>/dev/null
$SSH_CMD "sudo cat /var/log/fail2ban.log 2>/dev/null || cat /var/log/fail2ban.log 2>/dev/null" \
    > "$TMP_F2B" 2>/dev/null

if [ ! -s "$TMP_ACCESS" ]; then
    echo '{"error": "로그 수집 실패 또는 빈 파일"}' >&2
    exit 1
fi

# 분석 실행 (JSON 출력)
python3 "$SCRIPT_DIR/analyze.py" \
    --hours "$HOURS" --json --blocklist "$TMP_BLOCKLIST" "$TMP_ACCESS" "$TMP_ERROR" "$TMP_F2B"
