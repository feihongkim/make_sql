#!/bin/bash
# tem_homepage 컨테이너 구동 스크립트 (호스트: alvinii-232)
# 
# 사용법:
#   ./run_tem_homepage.sh

# 호스트 중앙화 API 키 로드
source ~/.api_keys 2>/dev/null

CONTAINER_NAME="tem_homepage"

# 기존 동일 이름 컨테이너 삭제
if [ "$(docker ps -a -q -f name=${CONTAINER_NAME})" ]; then
    echo "기존 ${CONTAINER_NAME} 컨테이너 중지 및 제거 중..."
    docker stop ${CONTAINER_NAME} 2>/dev/null
    docker rm ${CONTAINER_NAME} 2>/dev/null
fi

echo "신규 ${CONTAINER_NAME} 컨테이너 기동 중 (Bridge 모드 8000:8000)..."
docker run -d \
  -it \
  --name ${CONTAINER_NAME} \
  -p 8000:8000 \
  --restart unless-stopped \
  -v /home/alvinii/code/tem_homepage:/workspace \
  -v /home/alvinii/code/tem_homepage/.telegram:/home/node/.telegram \
  -v /home/alvinii/.claude.json:/home/node/.claude.json:rw \
  -v /home/alvinii/.claude/.credentials.json:/home/node/.claude/.credentials.json:rw \
  -e TELEGRAM_BOT_TOKEN="홈페이지 전용 새 봇 토큰" \
  -e DEEPSEEK_API_KEY \
  -e PERPLEXITY_API_KEY="" \
  -e GLM_API_KEY="" \
  -e ANTHROPIC_API_KEY="" \
  -e OPENAI_API_KEY="" \
  -e GEMINI_API_KEY="" \
  api_pi:11 \
  pi --provider deepseek --model deepseek-v4-pro

echo "완료: ${CONTAINER_NAME} 컨테이너가 성공적으로 기동되었습니다."
