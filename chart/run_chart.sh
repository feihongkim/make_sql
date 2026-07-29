#!/bin/bash
#
# 차트 컨테이너 기동. 조회 전용 DB 계정을 ~/.secrets 에서 읽어 넘긴다.
#
#   ./chart/run_chart.sh            빌드 + 기동
#   ./chart/run_chart.sh restart    재시작만
#
# 비밀값을 명령행이나 compose 파일에 적지 않는다. 환경변수로만 전달한다.

set -euo pipefail

SECRETS=${SECRETS:-$HOME/.secrets/mssql_chart_ro.env}
cd "$(dirname "$0")"

if [ ! -f "$SECRETS" ]; then
  echo "오류: 조회 전용 계정 파일이 없습니다 — $SECRETS" >&2
  echo "      MSSQL_CHART_RO_USER / MSSQL_CHART_RO_PASSWORD 를 담은 파일이 필요합니다." >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$SECRETS"
set +a

: "${MSSQL_CHART_RO_USER:?SECRETS 에 MSSQL_CHART_RO_USER 가 없습니다}"
: "${MSSQL_CHART_RO_PASSWORD:?SECRETS 에 MSSQL_CHART_RO_PASSWORD 가 없습니다}"

case "${1:-up}" in
  restart) docker compose restart ;;
  down)    docker compose down ;;
  *)       docker compose up -d --build ;;
esac

echo
echo "확인:"
echo "  docker exec makesql_chart curl -sS http://127.0.0.1:8800/health   # 등록된 소스"
echo "  ./abledb chart kor_daily 005930 --from 20260101 --to 20260720"
