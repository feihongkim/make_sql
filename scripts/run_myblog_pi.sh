#!/bin/bash
# myblog_pi 컨테이너 구동 스크립트 (호스트: white)
# 
# 사용법:
#   ./run_myblog_pi.sh

# 호스트 중앙화 API 키 로드
source ~/.api_keys 2>/dev/null

CONTAINER_NAME="myblog_pi"

# 기존 동일 이름 컨테이너 삭제
if [ "$(docker ps -a -q -f name=${CONTAINER_NAME})" ]; then
    echo "기존 ${CONTAINER_NAME} 컨테이너 중지 및 제거 중..."
    docker stop ${CONTAINER_NAME} 2>/dev/null
    docker rm ${CONTAINER_NAME} 2>/dev/null
fi

echo "신규 ${CONTAINER_NAME} 컨테이너 기동 중 (Bridge 모드)..."
docker run -d \
  -it \
  --name ${CONTAINER_NAME} \
  -p 3000:3000 \
  --restart unless-stopped \
  -v /home/feihong/Code/my-blog:/workspace \
  -v /home/feihong/Code/my-blog/.telegram:/home/node/.telegram \
  -v /home/feihong/.claude.json:/home/node/.claude.json:rw \
  -e TELEGRAM_BOT_TOKEN="7723743534 전용 블로그 봇 토큰" \
  -e PERPLEXITY_API_KEY \
  -e GLM_API_KEY \
  -e ANTHROPIC_API_KEY \
  -e OPENAI_API_KEY \
  -e DEEPSEEK_API_KEY \
  -e GEMINI_API_KEY \
  myblog_pi:1

echo "완료: ${CONTAINER_NAME} 컨테이너가 성공적으로 기동되었습니다."
echo "※ 주의: 컨테이너 최초 진입 시 Next.js 가동 명령(npm run serve -- -p 3000 -H 0.0.0.0)을 수행하십시오."
