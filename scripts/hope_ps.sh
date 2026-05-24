#!/bin/bash
# 우리 프로세스(_Hope + KIS) 상태 확인 스크립트
# 사용법: bash scripts/hope_ps.sh

TMPFILE=$(mktemp)
ps aux | grep -E "([_]Hope|^\S+\s+\S+\s.*\./KIS )" | grep -v -E "(grep|/bin/bash -c)" > "$TMPFILE"

awk '{
  cmd = $11
  for (i=12; i<=NF; i++) cmd = cmd " " $i
  # _Hope 제거, 경로 정리
  gsub(/_Hope/, "", cmd)
  gsub(/\.\/publish\//, "", cmd)
  gsub(/^dotnet /, "", cmd)
  gsub(/\.dll$/, "", cmd)
  gsub(/^\.\//, "", cmd)
  printf "%s|%s|%s|%.1f\n", cmd, $2, $3, $6/1024
}' "$TMPFILE" | sort | awk -F'|' '
BEGIN {
  printf "%-35s %7s %6s %8s\n", "프로세스", "PID", "CPU%", "MEM(MB)"
  printf "%-35s %7s %6s %8s\n", "-----------------------------------", "-------", "------", "--------"
}
{
  printf "%-35s %7s %5.1f%% %7.1f\n", $1, $2, $3, $4
}
END {
  printf "\n총 %d개 프로세스 가동 중\n", NR
}'

rm -f "$TMPFILE"
