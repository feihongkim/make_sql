#!/bin/bash
# MC_pi 컨테이너 구동 스크립트 (호스트: white)
# 
# 사용법:
#   ./run_MC_pi.sh

# 호스트 중앙화 API 키 로드
source ~/.api_keys 2>/dev/null

CONTAINER_NAME="MC_pi"

# 기존 동일 이름 컨테이너 삭제
if [ "$(docker ps -a -q -f name=${CONTAINER_NAME})" ]; then
    echo "기존 ${CONTAINER_NAME} 컨테이너 중지 및 제거 중..."
    docker stop ${CONTAINER_NAME} 2>/dev/null
    docker rm ${CONTAINER_NAME} 2>/dev/null
fi

echo "신규 ${CONTAINER_NAME} 컨테이너 기동 중 (Host 모드, Docker.sock 마운트)..."
docker run -d \
  -it \
  --name ${CONTAINER_NAME} \
  --network host \
  --restart unless-stopped \
  -v /home/feihong/code/MC:/workspace \
  -v /home/feihong/code/MC/.telegram:/home/node/.telegram \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v MC_pi_data_v1:/home/node \
  -e TELEGRAM_BOT_TOKEN="8971714582:AAECWmwytdccFXJeodP35s4jhKDmGI3vRJ4" \
  -e PERPLEXITY_API_KEY \
  -e GLM_API_KEY \
  -e ANTHROPIC_API_KEY \
  -e OPENAI_API_KEY \
  -e DEEPSEEK_API_KEY \
  -e GEMINI_API_KEY \
  api_pi:11 \
  pi --provider deepseek --model deepseek-v4-pro

echo "완료: ${CONTAINER_NAME} 컨테이너가 성공적으로 기동되었습니다."
