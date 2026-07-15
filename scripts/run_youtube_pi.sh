#!/bin/bash
# youtube_pi 컨테이너 구동 스크립트 (호스트: white)
#
# 사용법:
#   ./scripts/run_youtube_pi.sh

set -euo pipefail

# 호스트 중앙화 API 키 로드
source ~/.api_keys 2>/dev/null || true

CONTAINER_NAME="youtube_pi"
IMAGE_NAME="api_pi:11"
DATA_VOLUME="youtube_pi_data_v1"
WORKSPACE_DIR="/home/feihong/code/youtubeContent"
TELEGRAM_DIR="${WORKSPACE_DIR}/.telegram"
TELEGRAM_EXTENSION_SOURCE_CONTAINER="${TELEGRAM_EXTENSION_SOURCE_CONTAINER:-rstudio_pi}"

: "${YOUTUBE_PI_TELEGRAM_BOT_TOKEN:?YOUTUBE_PI_TELEGRAM_BOT_TOKEN missing in ~/.api_keys}"

mkdir -p "${TELEGRAM_DIR}"

# 기존 동일 이름 컨테이너 삭제
if [ "$(docker ps -a -q -f name=^/${CONTAINER_NAME}$)" ]; then
    echo "기존 ${CONTAINER_NAME} 컨테이너 중지 및 제거 중..."
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true
fi

echo "신규 ${CONTAINER_NAME} 컨테이너 기동 중 (Host 모드)..."
docker run -d \
  -it \
  --name "${CONTAINER_NAME}" \
  --network host \
  --restart unless-stopped \
  -v "${WORKSPACE_DIR}:/workspace" \
  -v "${TELEGRAM_DIR}:/home/node/.telegram" \
  -v /home/feihong/.claude.json:/home/node/.claude.json:rw \
  -v "${DATA_VOLUME}:/home/node" \
  -e TELEGRAM_BOT_TOKEN="${YOUTUBE_PI_TELEGRAM_BOT_TOKEN}" \
  -e PERPLEXITY_API_KEY \
  -e GLM_API_KEY \
  -e ANTHROPIC_API_KEY \
  -e OPENAI_API_KEY \
  -e DEEPSEEK_API_KEY \
  -e GEMINI_API_KEY \
  "${IMAGE_NAME}" \
  pi --provider deepseek --model deepseek-v4-pro

# Telegram extension bootstrap
if ! docker exec -u node "${CONTAINER_NAME}" test -d /home/node/.pi/agent/extensions/telegram; then
    echo "Telegram extension이 없어 ${TELEGRAM_EXTENSION_SOURCE_CONTAINER}에서 복사 중..."
    if ! docker ps --format '{{.Names}}' | grep -qx "${TELEGRAM_EXTENSION_SOURCE_CONTAINER}"; then
        echo "ERROR: Telegram extension source container not running: ${TELEGRAM_EXTENSION_SOURCE_CONTAINER}" >&2
        exit 1
    fi

    TMP_DIR="$(mktemp -d)"
    trap 'rm -rf "${TMP_DIR}"' EXIT

    docker cp "${TELEGRAM_EXTENSION_SOURCE_CONTAINER}:/home/node/.pi/agent/extensions/telegram" "${TMP_DIR}/"
    docker exec -u node "${CONTAINER_NAME}" mkdir -p /home/node/.pi/agent/extensions/telegram
    docker cp "${TMP_DIR}/telegram/." "${CONTAINER_NAME}:/home/node/.pi/agent/extensions/telegram/"

    echo "Telegram extension 복사 완료. ${CONTAINER_NAME} 재시작 중..."
    docker restart "${CONTAINER_NAME}" >/dev/null
fi

echo "완료: ${CONTAINER_NAME} 컨테이너가 성공적으로 기동되었습니다."
echo "검증: docker logs ${CONTAINER_NAME} 2>&1 | grep -i telegram"
