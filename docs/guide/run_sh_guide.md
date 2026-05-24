# run.sh 작성 가이드

서비스 실행 스크립트 표준 패턴. 빌드 + 실행 + KST 타임스탬프 로그를 하나의 스크립트로 관리한다.

## 네이밍 규칙

바이너리/프로세스명에 `_Hope` 접미사를 붙여서, `ps aux | grep "_Hope"`로 실행 중인 프로세스를 한 번에 확인할 수 있게 한다.

```bash
# Go:     go build -o <모듈명>_Hope .
# C#:     dotnet publish -o ./publish → ./publish/<프로젝트명>_Hope
# Python: python <프로젝트명>_Hope.py (심볼릭 링크 또는 래퍼)
```

```bash
# 전체 프로세스 확인
ps aux | grep "_Hope"
```

## 기본 구조

```bash
#!/bin/bash
# 서비스 실행 스크립트
# 사용법: ./run.sh

# 프로젝트명 (go.mod의 module명, .csproj명, 디렉토리명 등)
APP_NAME="MyProject"
BIN_NAME="${APP_NAME}_Hope"

# 1. 로그 디렉토리 생성
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

# 2. 빌드 (실패 시 종료)
echo "Building ${BIN_NAME}..."
<빌드 명령어> || { echo "Build failed"; exit 1; }

# 3. KST 타임스탬프 생성 (YYMMDDHHMM)
TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${APP_NAME}.log"

# 4. 실행 + 콘솔 출력 + 로그 파일 저장
echo "${BIN_NAME} started → ${LOG_FILE}"
./${BIN_NAME} 2>&1 | tee -a "${LOG_FILE}"
```

## 핵심 요소

| 요소 | 설명 |
|------|------|
| `_Hope` 접미사 | 프로세스 식별용. `ps aux \| grep "_Hope"`로 전체 확인 |
| `mkdir -p` | 로그 디렉토리 자동 생성 |
| `\|\| { echo ...; exit 1; }` | 빌드 실패 시 실행 방지 |
| `TZ='Asia/Seoul' date +"%y%m%d%H%M"` | KST 기준 타임스탬프 (예: 2604071052) |
| `2>&1 \| tee -a` | stdout/stderr 모두 콘솔 출력 + 파일 저장 |
| 로그 파일 접미사 | `_saver`, `_scheduler` 등으로 용도 구분 |

## 실행 모드

### 포그라운드 (콘솔 로그 출력, Ctrl+C로 종료)
```bash
./${BIN_NAME} 2>&1 | tee -a "${LOG_FILE}"
```

### 백그라운드 (터미널 종료해도 유지)
```bash
nohup ./${BIN_NAME} >> "${LOG_FILE}" 2>&1 &
echo "Started (PID: $!) → ${LOG_FILE}"
```

### 백그라운드 + 실시간 로그 확인
```bash
./${BIN_NAME} >> "${LOG_FILE}" 2>&1 &
tail -f "${LOG_FILE}"
```

## 언어별 예시

### Go

```bash
#!/bin/bash
APP_NAME="Saver"
BIN_NAME="${APP_NAME}_Hope"
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

echo "Building ${BIN_NAME}..."
go build -o "${BIN_NAME}" . || { echo "Build failed"; exit 1; }

TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${APP_NAME}.log"

echo "${BIN_NAME} started → ${LOG_FILE}"
./"${BIN_NAME}" 2>&1 | tee -a "${LOG_FILE}"
```

서브커맨드가 있는 경우:
```bash
#!/bin/bash
APP_NAME="KIS"
BIN_NAME="${APP_NAME}_Hope"
CMD=${1:-"server"}
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

echo "Building ${BIN_NAME}..."
go build -o "${BIN_NAME}" . || { echo "Build failed"; exit 1; }

TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${CMD}.log"

echo "${BIN_NAME} ${CMD} started → ${LOG_FILE}"
./"${BIN_NAME}" ${CMD} "${@:2}" 2>&1 | tee -a "${LOG_FILE}"
```

사용법:
```bash
./run.sh                          # → KIS_Hope server  → test_logs/2604071052_server.log
./run.sh scheduler                # → KIS_Hope scheduler → test_logs/2604071052_scheduler.log
./run.sh collect FG:NAS,NYS,AMS   # → KIS_Hope collect  → test_logs/2604071052_collect.log
```

### Python

```bash
#!/bin/bash
APP_NAME="DrawChartPy"
BIN_NAME="${APP_NAME}_Hope"
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

# conda 가상환경 활성화
eval "$(conda shell.bash hook)"
conda activate "${APP_NAME}"

TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${APP_NAME}.log"

echo "${BIN_NAME} started → ${LOG_FILE}"
python main.py 2>&1 | tee -a "${LOG_FILE}"
```

Python은 프로세스명이 `python`으로 표시되므로, `_Hope` 식별을 위해 심볼릭 링크를 활용할 수 있다:
```bash
# 심볼릭 링크로 프로세스명 변경
ln -sf main.py "${BIN_NAME}.py"
python "${BIN_NAME}.py" 2>&1 | tee -a "${LOG_FILE}"

# 이렇게 하면 ps aux | grep "_Hope" 로 찾을 수 있음
```

FastAPI/uvicorn 서버:
```bash
#!/bin/bash
APP_NAME="MyAPI"
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

eval "$(conda shell.bash hook)"
conda activate "${APP_NAME}"

TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${APP_NAME}.log"

echo "${APP_NAME}_Hope started → ${LOG_FILE}"
uvicorn app.main:app --host 0.0.0.0 --port 8000 2>&1 | tee -a "${LOG_FILE}"
```

### C# (.NET)

```bash
#!/bin/bash
APP_NAME="MyApp"
BIN_NAME="${APP_NAME}_Hope"
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

echo "Building ${BIN_NAME}..."
dotnet publish -c Release -r linux-x64 --self-contained \
    -p:AssemblyName="${BIN_NAME}" -o ./publish || { echo "Build failed"; exit 1; }

TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${APP_NAME}.log"

echo "${BIN_NAME} started → ${LOG_FILE}"
./publish/"${BIN_NAME}" 2>&1 | tee -a "${LOG_FILE}"
```

framework-dependent 배포:
```bash
#!/bin/bash
APP_NAME="MyApp"
BIN_NAME="${APP_NAME}_Hope"
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

echo "Building ${BIN_NAME}..."
dotnet build -c Release -p:AssemblyName="${BIN_NAME}" -o ./publish \
    || { echo "Build failed"; exit 1; }

TIMESTAMP=$(TZ='Asia/Seoul' date +"%y%m%d%H%M")
LOG_FILE="${LOG_DIR}/${TIMESTAMP}_${APP_NAME}.log"

echo "${BIN_NAME} started → ${LOG_FILE}"
dotnet ./publish/"${BIN_NAME}".dll 2>&1 | tee -a "${LOG_FILE}"
```

## 프로세스 관리

```bash
# 전체 _Hope 프로세스 확인
ps aux | grep "_Hope"

# 특정 서비스 확인
ps aux | grep "Saver_Hope"
ps aux | grep "KIS_Hope"

# 프로세스 종료
pkill -f Saver_Hope
pkill -f KIS_Hope

# 전체 _Hope 프로세스 종료
pkill -f "_Hope"
```

## .gitignore 설정

```gitignore
# 로그
test_logs/

# 빌드 산출물 (_Hope 바이너리)
*_Hope
*_Hope.exe
publish/
```

## 참고

- tmux/screen 안에서 실행하면 nohup 없이도 터미널 종료 시 프로세스 유지됨
- 로그 파일명의 접미사로 용도를 구분하면 여러 서비스를 한 서버에서 관리하기 편함
- `tee -a`의 `-a`는 append 모드 — 같은 타임스탬프로 재실행해도 로그가 덮어쓰이지 않음
- `ps aux | grep "_Hope"`로 전체 서비스 상태를 한눈에 파악 가능
