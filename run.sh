#!/bin/bash
# MakeSQL 서비스 실행 스크립트
# 사용법: ./run.sh [서브커맨드] [인자...]
# 예시:
#   ./run.sh                    → abledb_Hope (기본: info)
#   ./run.sh scheduler          → abledb_Hope scheduler
#   ./run.sh log-analyze white 3 → abledb_Hope log-analyze white 3

APP_NAME="abledb"
BIN_NAME="${APP_NAME}_Hope"
CMD=${1:-""}
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

# Go 환경 변수 설정
export PATH=$PATH:/usr/local/go/bin

# 1. 빌드
echo "Building ${BIN_NAME}..."
go build -o "${BIN_NAME}" . || { echo "Build failed"; exit 1; }

# 2. KST 타임스탬프
TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")

# 3. 로그 파일명 결정
if [ -z "$CMD" ]; then
    LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${APP_NAME}.log"
    echo "${BIN_NAME} started → ${LOG_FILE}"
    ./"${BIN_NAME}" 2>&1 | tee -a "${LOG_FILE}"
else
    LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${CMD}.log"
    echo "${BIN_NAME} ${CMD} started → ${LOG_FILE}"
    ./"${BIN_NAME}" ${CMD} "${@:2}" 2>&1 | tee -a "${LOG_FILE}"
fi
