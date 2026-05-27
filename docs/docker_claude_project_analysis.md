# Docker Claude 프로젝트 분석 보고서

> 분석일: 2026-05-27
> 방법: MakeSQL Claude → tg_send.py로 15개 봇에 "이 프로젝트를 분석해 줘" 전송 → tg_monitor.py로 응답 수집

---

## 프로젝트 요약

| # | Container | 언어 | 프로젝트 요약 |
|---|---|---|---|
| 1 | dart_claude | Go | OpenDART API 파이프라인. 83개 API, 3,946개 상장기업 공시→MSSQL 저장. 이상탐지 스캐너 |
| 2 | kis2_claude | Go | 한국투자증권 API. 330개 API, 170개 파이프라인, 7단계 스케줄러, 5개 퀀트 전략 |
| 3 | upbit_claude | Go | Upbit 암호화폐 CLI (7,135줄). REST/WebSocket, 12개 타임프레임, MA/RSI 전략 |
| 4 | restg_claude | C# | 주식 신호 분석 서비스. 12개 매수 전략 (DefBox/MainBox), 5단계 매도 로직 |
| 5 | restgo_claude | Go | MSSQL 다중DB 쿼리 도구. 4개 DB, RabbitMQ 로깅, Telegram 알림, AES-256 암호화 |
| 6 | saver_claude | Go | 중앙 로깅·영속성 서비스. RabbitMQ saveSt1→MSSQL 저장, 재시도 전략(지수 백오프) |
| 7 | dbsender_claude | C# | SQL Server 주식 데이터→RabbitMQ/Redis 배포. 6가지 모드, 국내/해외/업비트 캔들 |
| 8 | stocktopreason_claude | Go | 급등/급락 종목 자동탐지. KIS2 DB→RabbitMQ CQIn(AI분석)→CQOut→News DB, 매일 05:00 |
| 9 | readjson_claude | Go+Python | RabbitMQ→캔들스틱 차트(PNG) 자동생성. MA/볼린저밴드/박스가격, 웹 갤러리 |
| 10 | claude_python_forme | Python | 금융 데이터 수집·분석·백테스트 (2,234줄). 8가지 전략, Fama-French, riskfolio 최적화 |
| 11 | rstudio_claude | R | 퀀트 파이낸스. 7종 백테스트 엔진, YAML 룰 기반, 2-Layer 복합전략, 24개 설정파일 |
| 12 | mkyoutube_claude | Python | 유튜브 자동 제작. MoviePy+FFmpeg, DALL-E/Flux/HeyGen/Veo AI 콘텐츠, 14개 프로젝트 |
| 13 | ls_claude | C# | LS증권 XingAPI 자동 트레이딩. 379개 TR (349개 자동생성), 36개 시간 스케줄 작업 |

---

## 외부 봇

| # | Bot | 이름 | 비고 |
|---|---|---|---|
| 14 | FeiBNS_bot | blog_react | 블로그 |
| 15 | Feifile_bot | Moodle | LMS |

---

## 언어별 분류

### Go (7개)
dart_claude, kis2_claude, upbit_claude, restgo_claude, saver_claude, stocktopreason_claude, readjson_claude(+Python)

### C# / .NET (3개)
restg_claude, dbsender_claude, ls_claude

### Python (2개)
claude_python_forme, mkyoutube_claude

### R (1개)
rstudio_claude

---

## 발견된 이슈

### upbit_claude — go build 에러
- console 초기화 설계 충돌로 빌드 실패
- 조치 필요: console 패키지 의존성 해결

### saver_claude — 3가지 이슈
- 순차처리 병목 (배치/병렬 처리 권장)
- DEBUG 로그 과다 (프로덕션 레벨 조정 필요)
- panic 위험 (에러 핸들링 개선 필요)

### dbsender_claude — 보안 문제
- SQL Injection 위험
- 평문 암호 저장
- 보안 코드 리뷰 필요

---

## 시스템 아키텍처

```
                    ┌─────────────────┐
                    │  MakeSQL Claude  │ ← 컨트롤 타워
                    │  (호스트 세션)    │
                    └────────┬────────┘
                             │ tg_send.py (Telethon MTProto)
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                     │
   ┌────┴────┐         ┌────┴────┐          ┌────┴────┐
   │  Go 7개  │         │ C# 3개  │          │ Py/R 3개│
   │ Dart     │         │ RESTG   │          │ pyForMe │
   │ KIS2     │         │ dbSend  │          │ mkYT    │
   │ Upbit    │         │ LS      │          │ rstudio │
   │ RESTGo   │         └─────────┘          └─────────┘
   │ Saver    │
   │ StockTop │
   │ ReadJson │
   └──────────┘
```

모든 Docker Claude 컨테이너: Go 1.24.4 + Python 3.12.13 기본 설치
RESTG, dbSender: 추가 .NET 9.0.314
rstudio: 별도 R 이미지 (rocker/rstudio)
