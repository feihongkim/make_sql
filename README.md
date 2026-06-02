# make_sql (abledb)

다중 데이터베이스 통합 CLI 도구. MSSQL, MongoDB, RabbitMQ 연동 및 Claude AI 자동화를 지원합니다.

## 빌드

```bash
go build -o abledb .
```

## CLI 명령

```
./abledb                                          MongoDB 연결 정보 출력
./abledb mssql [--readonly] [서버] --dblist        MSSQL DB 목록
./abledb mssql [--readonly] [서버] [DB] [쿼리|@파일] MSSQL 쿼리 실행
./abledb mongo [연결] --dblist                     MongoDB DB 목록
./abledb mongo [연결] --drop-before [날짜]          날짜 이전 컬렉션 삭제
./abledb mongo [연결] [DB] [JSON|@파일]             MongoDB 명령 실행
./abledb log-analyze [연결] [시간(h)]              MongoDB LOG 분석
./abledb claude [프로젝트명] [프롬프트|@파일]        로컬 프로젝트 Claude 실행
./abledb docker-claude [컨테이너명] [프롬프트]      Docker 컨테이너 Claude 실행
./abledb send [컨테이너명] [프롬프트|@파일]          Docker Claude 세션에 메시지 전송
./abledb surge-report [YYYYMMDD[-YYYYMMDD]] [옵션]  급등 종목 분석 MD 생성
./abledb copy [소스] [소스DB] [대상] [대상DB] ...   데이터 복사
./abledb security-check                            서버 보안 점검
./abledb scheduler [status|stop]                  스케줄러 실행/관리
```

## 데이터베이스

| 종류 | 대상 |
|------|------|
| MSSQL | TUF, ITWdesk, white |
| MongoDB (Tailscale) | tuf, white, itwdesk |
| Docker DB | MySQL, MariaDB, PostgreSQL, Oracle 19c, Redis |

## 스케줄러

`./abledb scheduler` 실행 시 자동으로 주기적 작업을 수행합니다:

- **매 3시간 :40** — MongoDB LOG 분석 → Telegram 전송
- **매 3시간 :10** — Nginx 로그 보안 분석 → Telegram 전송
- **매 3시간 :30** — 서버 보안 점검 (4대)
- **매일 15:31** — 급등 종목 MDX 동기화
- **매시 :17** — Notion 블로그 포스트 동기화

## 설정 파일

| 파일 | 설명 |
|------|------|
| `config.yaml` | DB 연결 정보 (AES 암호화) |
| `claude_project.yaml` | Claude 로컬 프로젝트 경로 매핑 |

## MCP 서버 (Python)

- `mcp/notion/server.py` — Notion API 연동
- `mcp/google/server.py` — Gmail + Google Calendar 연동

## 의존성

- [go-mssqldb](https://github.com/denisenkom/go-mssqldb) — MSSQL 드라이버
- [mongo-driver](https://github.com/mongodb/mongo-go-driver) — MongoDB 드라이버
- [amqp091-go](https://github.com/rabbitmq/amqp091-go) — RabbitMQ 클라이언트
- [zap](https://github.com/uber-go/zap) — 구조화 로깅
