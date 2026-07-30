FROM python:3.12-slim

# 한글 종목명·제목이 네모로 깨지지 않게 폰트를 이미지에 넣는다.
# 호스트 폰트에 의존하면 "내 PC 에선 되는데" 가 생긴다.
# fontconfig 을 함께 깔아야 한다. slim 이미지에는 fc-cache 가 없어서
# 빠뜨리면 이 RUN 이 exit 100 으로 실패한다.
RUN apt-get update && apt-get install -y --no-install-recommends \
        fonts-nanum fontconfig curl \
    && rm -rf /var/lib/apt/lists/* \
    && fc-cache -f

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY render.py server.py ./
# config/ 는 마운트한다. 소스를 추가할 때 재빌드하지 않기 위해서다.
# 파일이 아니라 디렉토리를 마운트해야 수정이 반영된다 (런북 8-2).

# boxcalc — RESTGo 의 Box 로직 정적 Go 바이너리 (CGO 없음, glibc 무의존).
# 갱신: RESTGo 저장소에서 deploy/build_boxcalc.sh 실행 → 여기 이미지 재빌드.
COPY bin/boxcalc /app/bin/boxcalc

ENV MPLBACKEND=Agg \
    PYTHONUNBUFFERED=1 \
    CHART_SOURCES=/app/config/chart_sources.yaml

EXPOSE 8800
CMD ["python", "server.py"]
