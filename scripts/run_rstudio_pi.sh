#!/bin/bash
# rstudio_pi 컨테이너 구동 스크립트 (호스트: white)
# 
# 사용법:
#   ./run_rstudio_pi.sh

# 호스트 중앙화 API 키 로드
source ~/.api_keys 2>/dev/null

CONTAINER_NAME="rstudio_pi"

# 기존 동일 이름 컨테이너 삭제
if [ "$(docker ps -a -q -f name=${CONTAINER_NAME})" ]; then
    echo "기존 ${CONTAINER_NAME} 컨테이너 중지 및 제거 중..."
    docker stop ${CONTAINER_NAME} 2>/dev/null
    docker rm ${CONTAINER_NAME} 2>/dev/null
fi

echo "신규 ${CONTAINER_NAME} 컨테이너 기동 중 (Host 모드)..."
docker run -d \
  -it \
  --name ${CONTAINER_NAME} \
  --network host \
  --restart unless-stopped \
  -v /data2/rstudio:/workspace \
  -v /data2/rstudio/.telegram:/home/node/.telegram \
  -v /home/feihong/.claude.json:/home/node/.claude.json:rw \
  -e TELEGRAM_BOT_TOKEN="8870072194:AAE_Df28_EvI6Y5l7shKBAdXhvX7_iKVkdE" \
  -e PERPLEXITY_API_KEY \
  -e GLM_API_KEY \
  -e ANTHROPIC_API_KEY \
  -e OPENAI_API_KEY \
  -e DEEPSEEK_API_KEY \
  -e GEMINI_API_KEY \
  rstudio_pi:1 \
  pi --provider deepseek --model deepseek-v4-pro

echo "완료: ${CONTAINER_NAME} 컨테이너가 성공적으로 기동되었습니다."
