#!/bin/bash
# nginx 로그를 원격 서버에서 가져와 보안 분석 후 JSON 출력
# 사용법: ./fetch_and_analyze.sh [시간수(기본2)]

HOURS=${1:-2}
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SSH_KEY="${SCRIPT_DIR}/../moodle.pem"
REMOTE_HOST="ubuntu@3.34.223.162"
TMP_ACCESS="/tmp/nginx_access_$$.log"
TMP_ERROR="/tmp/nginx_error_$$.log"
TMP_F2B="/tmp/nginx_fail2ban_$$.log"
TMP_BLOCKLIST="/tmp/nginx_blocklist_$$.conf"
TMP_ANALYSIS="/tmp/nginx_analysis_$$.json"
AUTO_BLOCK_HISTORY="${SCRIPT_DIR}/auto_block_history.jsonl"

cleanup() {
    rm -f "$TMP_ACCESS" "$TMP_ERROR" "$TMP_F2B"
    rm -f "$TMP_BLOCKLIST" "$TMP_ANALYSIS"
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

# 미차단 위협 분석 결과 생성
python3 "$SCRIPT_DIR/analyze.py" \
    --hours "$HOURS" --json --blocklist "$TMP_BLOCKLIST" "$TMP_ACCESS" "$TMP_ERROR" "$TMP_F2B" \
    > "$TMP_ANALYSIS"

# 명백한 공격자만 30일 임시 차단 후 결과 JSON에 자동 차단 내역을 첨부한다.
AUTO_BLOCK_ARGS=(
    --analysis "$TMP_ANALYSIS"
    --blocklist "$TMP_BLOCKLIST"
    --ssh-key "$SSH_KEY"
    --remote "$REMOTE_HOST"
    --history "$AUTO_BLOCK_HISTORY"
)
if [[ "${AUTO_BLOCK_DRY_RUN:-0}" == "1" ]]; then
    AUTO_BLOCK_ARGS+=(--dry-run)
fi
python3 "$SCRIPT_DIR/auto_block.py" "${AUTO_BLOCK_ARGS[@]}"

cat "$TMP_ANALYSIS"
