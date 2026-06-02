# MakeSQL Claude (Opus 4.6) — 능력 총정리

이 문서는 MakeSQL 프로젝트의 Claude가 수행할 수 있는 모든 작업을 정리한다.

---

## 1. 데이터베이스 접근

### MSSQL
- **TUF, ITWdesk, white** 서버 접속 및 쿼리 실행
- DB 목록 조회, SELECT/INSERT/UPDATE/DELETE 쿼리 직접 실행
- `./abledb mssql [서버] [DB] [쿼리|@파일]`

### MongoDB
- **TUF, white, ITWdesk** (Tailscale VPN 네트워크)
- DB/컬렉션 조회, aggregate, find, insertMany 등 명령 실행
- LOG 분석 (시간 범위 기반)
- `./abledb mongo [연결] [DB] [JSON|@파일]`

### 로컬 Docker DB
- MySQL, MariaDB, PostgreSQL, Oracle 19c, Redis 접근 가능

---

## 2. Docker Claude 컨테이너 제어 (15대)

### abledb send를 통한 메시지 전송
기존 Docker Claude 세션에 1회성 프롬프트를 전송하고 결과를 받을 수 있다.
- `./abledb send [컨테이너명] [프롬프트]`
- Telegram 봇 세션을 죽이지 않는 안전한 실행 방식

| 컨테이너 | 워크스페이스 |
|---|---|
| dart_claude | /home/feihong/code/Dart |
| kis2_claude | /home/feihong/code/KIS2 |
| upbit_claude | /home/feihong/code/Upbit |
| restg_claude | /home/feihong/code/REST/RESTG |
| restgo_claude | /home/feihong/code/REST/RESTGo |
| saver_claude | /home/feihong/code/Saver |
| dbsender_claude | /home/feihong/code/dbSender |
| stocktopreason_claude | /home/feihong/code/StockTopReason |
| readjson_claude | /home/feihong/code/ReadJson |
| claude_python_forme | /home/feihong/code/python_ForMe |
| rstudio_claude | /data2/rstudio |
| mkyoutube_claude | /data1/mkYoutube |
| ls_claude | Windows SSH 접속 |
| makesql_claude | /home/feihong/code/MakeSQL |
| claude_ticker | /home/feihong/code/Ticker |

### Docker 컨테이너 관리
- 컨테이너 상태 확인 (docker ps)
- 컨테이너 재시작 (docker compose restart)
- 새 Docker Claude 생성 (`new_docker_claude.sh`)
- 프로세스 모니터링 (bot.pid 확인)

---

## 3. 원격 서버 접근 (SSH)

| 서버 | 주소 | 사용자 |
|---|---|---|
| white (현재 호스트) | 192.168.3.120 | feihong |
| 117 | 192.168.3.117 | feihong |
| 130 | 192.168.3.130 | feihong |
| 232 | 192.168.3.232:2222 | alvinii |
| Lightsail | AWS | SSH key |

### 수행 가능한 작업
- 서비스 상태 확인 (systemctl, ps, docker)
- 방화벽 관리 (iptables, ufw)
- 패키지 관리 (apt)
- 로그 분석 (journalctl, /var/log)
- Nginx 설정 수정 및 IP 차단 (Lightsail)
- 네트워크 진단 (ss, netstat, curl)

---

## 4. 보안 관리

### 서버 보안 점검
- 4대 서버 자동 보안 점검 (`./abledb security-check`)
- 열린 포트, 실패한 로그인 시도, 서비스 상태 분석
- 스케줄러를 통한 정기 점검

### Nginx 보안 (Lightsail)
- 액세스 로그 분석 (공격 패턴 탐지)
- 악성 IP 차단 (geo_blocklist.conf — geo 모듈, 5,600+개 IP/서브넷)
- 취약점 스캔 요청 필터링

---

## 5. 외부 서비스 (MCP 연동)

### Telegram
- 메시지 수신 및 회신
- 파일/이미지 첨부 전송
- 리액션 추가
- 메시지 편집

### Notion
- 페이지/데이터베이스 검색
- 페이지 생성/수정
- 댓글 작성

### Gmail
- 메일 검색 (스레드 조회)
- 라벨 관리
- 초안 작성

### Google Calendar
- 일정 조회/생성/수정/삭제
- 빈 시간 확인 (suggest_time)
- 이벤트 응답

---

## 6. 프로젝트 코드 작업

### 로컬 프로젝트 (18개)
`claude_project.yaml`에 등록된 프로젝트에 대해 코드 분석/수정 가능:
Admin, Analysis, Dart, dbSender, Input, KIS2, Logger, MakeSQL, MC, python_ForMe, ReadJson, RESTG, RESTGo, Saver, StockTopReason, Ticker, Upbit

### 수행 가능한 작업
- 코드 읽기/분석/수정
- 버그 수정, 기능 추가
- Go 빌드 및 테스트
- Git 커밋/브랜치 관리
- 문서 생성

---

## 7. 스케줄러 / 자동화

- `./abledb scheduler` — 정기 작업 실행
  - log_analyze: 매 3시간 :40 — MongoDB LOG 분석 → Telegram 전송
  - nginx_analyze: 매 3시간 :10 — Nginx 로그 보안 분석 → Telegram 전송
  - security_check: 매 3시간 :30 — 4대 서버 보안 점검
  - surge_sync: 매일 15:31 — 급등 종목 MDX 동기화 (alvinii 배포)
  - blog_sync: 매시 :17 — Notion 블로그 포스트 동기화
  - temp_check: 매 3시간 :50 — white/117/130 서버 온도 점검
- 서버 재부팅 시 crontab @reboot로 자동 시작
- `_Hope` 서비스 프로세스 모니터링

---

## 8. 데이터 복사

- MSSQL ↔ MSSQL 간 테이블 데이터 복사
- `./abledb copy [소스] [소스DB] [대상] [대상DB] [테이블] [조건]`

---

## 9. 멀티 에이전트 협업

MakeSQL Claude가 **컨트롤 타워** 역할을 수행:
1. 사용자가 Telegram으로 요청
2. MakeSQL Claude가 적합한 Docker Claude에 작업 위임 (abledb send)
3. Docker Claude가 자신의 프로젝트 컨텍스트에서 작업 수행
4. 결과를 MakeSQL Claude가 수집하여 사용자에게 회신

이를 통해 하나의 Telegram 대화창에서 15개 프로젝트를 동시에 관리할 수 있다.
