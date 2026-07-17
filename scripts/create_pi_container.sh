#!/bin/bash

PREFIX=$1
BASE_FOLDER=$2
DOCS_FOLDER=$3
PORT_MAPPING=$4

if [ -z "$PREFIX" ] || [ -z "$BASE_FOLDER" ]; then
  echo "Usage: $0 <PREFIX> <BASE_FOLDER_PATH> [DOCS_FOLDER] [PORT_MAPPING]"
  echo "Example 1 (Auto docs): $0 restgo RESTGo"
  echo "Example 2 (Auto docs + Port): $0 restgo RESTGo 8080:8080"
  echo "Example 3 (Manual docs): $0 restgo REST/RESTGo RESTGo 8080:8080"
  exit 1
fi

# 3번째 인자가 포트 번호이거나 콜론을 포함한 포트 매핑일 경우 자동 쉬프트
if [[ "$DOCS_FOLDER" == *":"* ]] || [[ "$DOCS_FOLDER" =~ ^[0-9]+$ ]]; then
  PORT_MAPPING=$DOCS_FOLDER
  DOCS_FOLDER=""
fi

if [ -z "$DOCS_FOLDER" ]; then
  DOCS_FOLDER=$(basename "$BASE_FOLDER")
fi

CONTAINER_NAME="${PREFIX}_pi"
TOKEN_FILE="${PREFIX}_pi.token"

# 토큰 파일 존재 여부 확인
TOKEN_PATH="/home/feihong/code/ContainerSetup/Tokens/${TOKEN_FILE}"
if [ "$SKIP_TOKEN" != "1" ]; then
  if [ ! -f "$TOKEN_PATH" ]; then
    echo "Error: Token file not found at ${TOKEN_PATH}"
    ls -1 /home/feihong/code/ContainerSetup/Tokens/
    exit 1
  fi
  TOKEN_MOUNT="-v ${TOKEN_PATH}:/home/node/.pi/agent/telegram_token:ro"
else
  echo "SKIP_TOKEN is set. Skipping telegram_token mount."
  TOKEN_MOUNT=""
fi

IMAGE_NAME=${PI_IMAGE:-"api_pi_2:12"}

WORKSPACE_PATH="/home/feihong/code/${BASE_FOLDER}"
if [[ "$BASE_FOLDER" == /* ]]; then
  WORKSPACE_PATH="$BASE_FOLDER"
fi

CMD="docker run -d -it --name ${CONTAINER_NAME} \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --memory=4g --cpus=2 \
  --network pinet"

if [ -n "$PORT_MAPPING" ]; then
  CMD="$CMD -p 0.0.0.0:${PORT_MAPPING}"
fi

CMD="$CMD \
  -v /home/feihong/code/ContainerSetup/PiExtensions/telegram:/home/node/.pi/agent/extensions/telegram:ro \
  -v /home/feihong/code/ContainerSetup/PiExtensions/api_server:/home/node/.pi/agent/extensions/api_server:ro \
  ${TOKEN_MOUNT} \
  -v /home/feihong/code/ContainerSetup/pi_keys.json:/home/node/.pi/agent/credentials:ro \
  -v ${WORKSPACE_PATH}:/workspace:rw \
  -v /home/feihong/code/Jarvis/project/${DOCS_FOLDER}:/docs:rw \
  -v ${CONTAINER_NAME}_data:/home/node \
  ${IMAGE_NAME} \
  pi --provider deepseek --model deepseek-v4-pro"

echo "====================================="
echo "실행할 Docker 명령어:"
echo "$CMD"
echo "====================================="

eval $CMD
