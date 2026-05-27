# 멀티 Docker Claude 트레이딩 시스템 구축 제안서

> 작성일: 2026-05-27
> 작성: MakeSQL Claude (컨트롤 타워)

---

## 1. 시스템 개요

15개 Docker Claude 컨테이너를 역할별로 조직하여, **데이터 수집 → 분석 → 신호 생성 → 주문 실행 → 모니터링**의 완전 자동화 트레이딩 파이프라인을 구축한다.

### 핵심 원칙

- **MakeSQL Claude**가 컨트롤 타워로서 전체 파이프라인 조율
- **RabbitMQ**를 메시지 버스로 활용하여 컨테이너 간 비동기 통신
- **MSSQL**을 중앙 데이터 저장소로 사용 (TUF/ITWdesk/white 3대)
- 각 Docker Claude는 자신의 전문 영역에 집중

---

## 2. 역할 분류

### 2-1. 증권사/거래소 연동 (데이터 수집 + 주문 실행)

| 컨테이너 | 역할 | 현재 역량 |
|---|---|---|
| **kis2_claude** | 한국투자증권 API | 330개 API, 170개 파이프라인, 7단계 스케줄러, 5개 퀀트 전략 |
| **ls_claude** | LS증권 XingAPI | 379개 TR (349개 자동생성), 36개 시간 스케줄 작업 |
| **upbit_claude** | Upbit 암호화폐 | REST/WebSocket, 12개 타임프레임, MA/RSI 전략 |

**이 그룹의 임무:**
1. 실시간/정기 시세 데이터 수집 → MSSQL 저장
2. 주문 신호 수신 시 실제 매매 실행
3. 체결/잔고 정보 RabbitMQ로 전파

### 2-2. 데이터베이스 계층

| 서버 | DB | 용도 |
|---|---|---|
| **white** (192.168.3.120) | MSSQL + MongoDB | 메인 데이터 저장, LOG 분석 |
| **ITWdesk** (192.168.3.117) | MSSQL + MongoDB | 보조 데이터, 백업 |
| **TUF** (192.168.3.130) | MSSQL + MongoDB | 분석용 데이터 |

**핵심 테이블 구조 (기존):**
- `KIS2_StockPrice` — 국내 주식 시세
- `KIS2_Balance` — 잔고
- `LS_*` — LS증권 데이터
- `Upbit_*` — 암호화폐 데이터
- `News` — 급등/급락 AI 분석 결과 (StockTopReason 산출)

### 2-3. 분석/전략 엔진

| 컨테이너 | 역할 | 현재 역량 |
|---|---|---|
| **restg_claude** | C# 매매 신호 분석 | 12개 매수 전략 (DefBox/MainBox), 5단계 매도 로직 |
| **rstudio_claude** | R 퀀트 백테스트 | 7종 백테스트 엔진, YAML 룰 기반, 2-Layer 복합전략, 24개 설정파일 |
| **claude_python_forme** | Python 분석/최적화 | 8가지 전략, Fama-French, riskfolio 포트폴리오 최적화 |

**이 그룹의 임무:**
1. DB에서 시세 데이터 읽기
2. 전략 로직 실행 → 매수/매도 신호 생성
3. 백테스트를 통한 전략 검증
4. 신호를 RabbitMQ로 전파

### 2-4. 지원 시스템

| 컨테이너 | 역할 | 현재 역량 |
|---|---|---|
| **dbsender_claude** | 데이터 배포/테스트 | 6가지 모드, 국내/해외/업비트 캔들 → RabbitMQ/Redis 배포 |
| **readjson_claude** | 차트 시각화 | RabbitMQ→캔들스틱 차트(PNG), MA/볼린저밴드/박스가격, 웹 갤러리 |
| **saver_claude** | 중앙 로깅/영속성 | RabbitMQ saveSt1→MSSQL, 재시도 전략(지수 백오프) |
| **stocktopreason_claude** | 급등/급락 분석 | KIS2 DB→RabbitMQ CQIn(AI분석)→CQOut→News DB |
| **dart_claude** | 공시 데이터 | 83개 API, 3,946개 상장기업 공시→MSSQL, 이상탐지 |

---

## 3. 데이터 흐름 아키텍처

```
┌─────────────────────────────────────────────────────────────────────┐
│                        MakeSQL Claude (컨트롤 타워)                   │
│                   tg_send.py / abledb docker-claude                  │
│                      스케줄러 / 모니터링 / 조율                        │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        ▼                      ▼                      ▼
 ═══ 수집 계층 ═══      ═══ 분석 계층 ═══      ═══ 지원 계층 ═══
 ┌──────────────┐      ┌──────────────┐      ┌──────────────┐
 │  kis2_claude │      │ restg_claude │      │readjson_claude│
 │  (KIS2 API)  │      │ (C# 전략)    │      │ (차트 생성)   │
 ├──────────────┤      ├──────────────┤      ├──────────────┤
 │  ls_claude   │      │rstudio_claude│      │ saver_claude │
 │  (LS API)    │      │ (R 백테스트)  │      │ (로깅/저장)   │
 ├──────────────┤      ├──────────────┤      ├──────────────┤
 │ upbit_claude │      │ pyForMe      │      │dbsender_claude│
 │  (Upbit API) │      │ (Python 최적) │      │ (데이터 배포)  │
 └──────┬───────┘      └──────┬───────┘      ├──────────────┤
        │                     │               │stocktopreason│
        ▼                     ▼               │ (AI 뉴스분석) │
 ┌──────────────────────────────┐             ├──────────────┤
 │         MSSQL (3대)          │             │ dart_claude  │
 │  white / ITWdesk / TUF       │             │ (공시 데이터)  │
 │  시세, 잔고, 체결, 전략결과    │             └──────────────┘
 └──────────────────────────────┘
        │                     │
        └──────────┬──────────┘
                   ▼
 ┌──────────────────────────────┐
 │        RabbitMQ (메시지 버스)  │
 │  saveSt1, CQIn, CQOut, LOG   │
 │  + 신규: TradeSignal, Order   │
 └──────────────────────────────┘
```

---

## 4. RabbitMQ 큐 설계

### 기존 큐 (유지)

| 큐 | 생산자 | 소비자 | 용도 |
|---|---|---|---|
| `saveSt1` | 각 컨테이너 | saver_claude | 범용 로그/데이터 저장 |
| `CQIn` | stocktopreason | Claude AI | 급등/급락 분석 요청 |
| `CQOut` | Claude AI | stocktopreason | 분석 결과 |
| `LOG` | 각 컨테이너 | MakeSQL | 운영 로그 |

### 신규 큐 (추가)

| 큐 | 생산자 | 소비자 | 용도 |
|---|---|---|---|
| `TradeSignal` | restg, rstudio, pyForMe | kis2, ls, upbit | 매매 신호 (종목, 방향, 수량, 가격) |
| `OrderResult` | kis2, ls, upbit | saver, MakeSQL | 주문 체결 결과 |
| `MarketData` | kis2, ls, upbit | restg, rstudio, pyForMe | 실시간 시세 스트림 |
| `PortfolioUpdate` | pyForMe | kis2, ls | 포트폴리오 리밸런싱 지시 |
| `AlertQueue` | 모든 컨테이너 | MakeSQL | 긴급 알림 (손절, 에러 등) |

### TradeSignal 메시지 포맷

```json
{
  "signal_id": "SIG-20260527-001",
  "timestamp": "2026-05-27T09:01:00+09:00",
  "source": "restg_claude",
  "strategy": "DefBox_Breakout",
  "market": "KRX",
  "broker": "kis2",
  "symbol": "005930",
  "name": "삼성전자",
  "action": "BUY",
  "quantity": 10,
  "price_type": "LIMIT",
  "price": 72500,
  "stop_loss": 70000,
  "take_profit": 78000,
  "confidence": 0.85,
  "backtest_sharpe": 1.42
}
```

---

## 5. 일일 운영 타임라인

```
04:00  dart_claude       공시 데이터 수집 (전일 공시 + 당일 예고)
05:00  stocktopreason    전일 급등/급락 AI 분석 실행
06:00  kis2_claude       전일 체결/잔고 정산, 당일 시세판 로드
       ls_claude         전일 체결/잔고 정산
       upbit_claude      24시간 시세 데이터 갱신
07:00  pyForMe           포트폴리오 최적화 실행 (Fama-French, riskfolio)
       rstudio_claude    백테스트 엔진 구동 (24개 YAML 설정 기반)
08:00  restg_claude      매매 전략 사전 스캐닝 (DefBox/MainBox)
       dbsender_claude   캔들 데이터 RabbitMQ/Redis 배포
08:30  readjson_claude   주요 종목 차트 생성 → 웹 갤러리 업데이트
─────── 장 시작 (09:00) ───────
09:00  kis2/ls           실시간 시세 수집 시작
       restg_claude      실시간 매매 신호 생성 → TradeSignal 큐
       kis2/ls           TradeSignal 소비 → 주문 실행
09:00~ saver_claude      모든 체결/신호/로그 MSSQL 저장
       MakeSQL           AlertQueue 모니터링 → Telegram 알림
─────── 장 마감 (15:30) ───────
15:30  kis2/ls           당일 체결 정산
16:00  rstudio_claude    당일 성과 분석 + 전략 파라미터 조정
       pyForMe           포트폴리오 리밸런싱 계산
17:00  readjson_claude   당일 매매 차트 생성
       MakeSQL           일일 리포트 생성 → Telegram/Notion
─────── 야간 ───────
22:00  upbit_claude      암호화폐 전략 실행 (24시간 시장)
```

---

## 6. 컨테이너별 구현 상세

### 6-1. kis2_claude (한국투자증권)

**현재 상태:** 330개 API, 170개 파이프라인, 7단계 스케줄러, 5개 퀀트 전략

**추가 구현:**
- `TradeSignal` 큐 소비자 추가 → 자동 주문 실행
- `OrderResult` 큐 발행 → 체결 결과 전파
- `MarketData` 큐 발행 → 실시간 시세 스트림
- 주문 실행 전 리스크 체크 (최대 포지션, 일일 손실 한도)

**리스크 관리 파라미터:**
```yaml
risk:
  max_position_per_stock: 5000000    # 종목당 최대 500만원
  max_daily_loss: 2000000            # 일일 최대 손실 200만원
  max_orders_per_day: 50             # 일일 최대 주문 50건
  force_stop_loss_pct: 3.0           # 강제 손절 -3%
```

### 6-2. ls_claude (LS증권)

**현재 상태:** 379개 TR, 36개 시간 스케줄 작업

**추가 구현:**
- kis2_claude와 동일한 TradeSignal 소비/OrderResult 발행 구조
- LS증권 전용 API 특성 반영 (XingAPI TR 코드 매핑)
- Windows SSH 접속 환경 고려한 안정성 강화

### 6-3. upbit_claude (Upbit)

**현재 상태:** REST/WebSocket, 12개 타임프레임, MA/RSI 전략 (빌드 에러 존재)

**선행 작업:** console 패키지 의존성 해결 → go build 정상화

**추가 구현:**
- WebSocket 실시간 시세 → MarketData 큐 발행
- 24시간 자동 매매 (암호화폐 시장)
- 전략: MA/RSI 기존 + 볼린저밴드/MACD 추가

### 6-4. restg_claude (매매 신호 엔진)

**현재 상태:** 12개 매수 전략 (DefBox/MainBox), 5단계 매도 로직

**추가 구현:**
- MarketData 큐 소비 → 실시간 전략 평가
- TradeSignal 큐 발행 (신호 생성)
- 전략별 성과 추적 테이블 (MSSQL)
- 전략 활성/비활성 동적 제어

**매수 전략 분류:**
| 전략군 | 전략 | 설명 |
|---|---|---|
| DefBox | 박스권 돌파 | 가격이 상단 돌파 시 매수 |
| MainBox | 메인 박스 | 핵심 가격대 지지/저항 기반 |
| 기타 | 10개 전략 | 각각 고유 로직 |

### 6-5. rstudio_claude (백테스트 엔진)

**현재 상태:** 7종 백테스트 엔진, YAML 룰 기반, 2-Layer 복합전략

**추가 구현:**
- 자동 백테스트: 새 전략 파라미터 → 과거 데이터 검증
- 결과를 MSSQL에 저장 + TradeSignal에 confidence 점수 포함
- 일일/주간 전략 성과 리포트 생성

### 6-6. claude_python_forme (포트폴리오 최적화)

**현재 상태:** 8가지 전략, Fama-French, riskfolio 최적화

**추가 구현:**
- 일일 포트폴리오 리밸런싱 계산
- PortfolioUpdate 큐 발행 → 증권사 컨테이너가 실행
- 리스크 팩터 모니터링 (시장 베타, 섹터 노출)

### 6-7. 지원 컨테이너

**dbsender_claude:**
- 보안 이슈 우선 해결 (SQL Injection, 평문 암호)
- 테스트 데이터 배포 역할 유지
- 백테스트용 과거 데이터 배포 추가

**readjson_claude:**
- 매매 신호 발생 시 해당 종목 차트 자동 생성
- 일일 매매 리포트 차트 생성
- Telegram으로 차트 이미지 전송

**saver_claude:**
- 선행 작업: 배치 처리 전환, 로그 레벨 조정, panic 핸들링
- 모든 TradeSignal/OrderResult를 MSSQL에 기록
- 감사 추적(audit trail) 완전성 보장

**stocktopreason_claude:**
- 기존 급등/급락 AI 분석 유지
- 분석 결과를 전략 엔진(restg)에 피드백

**dart_claude:**
- 공시 데이터 기반 이벤트 드리븐 신호
- 실적 발표, 유상증자, 자사주 매입 등 → 전략 엔진 피드

---

## 7. MakeSQL 컨트롤 타워 역할

```go
// abledb trading 명령 추가 예시
./abledb trading status          // 전체 시스템 상태
./abledb trading signal-log      // 최근 매매 신호 조회
./abledb trading performance     // 수익률 리포트
./abledb trading pause           // 전체 매매 일시 중지
./abledb trading resume          // 매매 재개
./abledb trading risk-check      // 리스크 현황
```

**컨트롤 타워 기능:**
1. **스케줄러 확장** — 기존 보안 점검/LOG 분석에 트레이딩 스케줄 추가
2. **AlertQueue 모니터링** — 손절/에러/이상 감지 시 Telegram 즉시 알림
3. **일일 리포트** — 수익률, 체결 내역, 전략 성과 → Telegram + Notion
4. **긴급 제어** — 전체 시스템 일시 중지/재개

---

## 8. 구현 로드맵

### Phase 1: 기반 정비 (1주)

- [ ] upbit_claude go build 에러 수정
- [ ] saver_claude 3가지 이슈 해결 (배치 처리, 로그 레벨, panic)
- [ ] dbsender_claude 보안 이슈 해결 (SQL Injection, 암호 암호화)
- [ ] RabbitMQ 신규 큐 생성 (TradeSignal, OrderResult, MarketData, PortfolioUpdate, AlertQueue)
- [ ] TradeSignal 메시지 포맷 표준화 (JSON 스키마)

### Phase 2: 데이터 파이프라인 (2주)

- [ ] kis2_claude: MarketData 큐 발행 구현
- [ ] ls_claude: MarketData 큐 발행 구현
- [ ] upbit_claude: WebSocket → MarketData 큐 발행
- [ ] restg_claude: MarketData 큐 소비 → 전략 평가 연동
- [ ] saver_claude: 신규 큐 메시지 MSSQL 저장 로직

### Phase 3: 전략 → 신호 → 실행 (2주)

- [ ] restg_claude: TradeSignal 발행 구현
- [ ] rstudio_claude: 자동 백테스트 → confidence 점수 생성
- [ ] pyForMe: 포트폴리오 최적화 → PortfolioUpdate 발행
- [ ] kis2_claude: TradeSignal 소비 → 주문 실행 (페이퍼 트레이딩)
- [ ] ls_claude: TradeSignal 소비 → 주문 실행 (페이퍼 트레이딩)

### Phase 4: 모니터링 & 리포트 (1주)

- [ ] MakeSQL: AlertQueue 모니터링 + Telegram 알림
- [ ] readjson_claude: 매매 신호 차트 자동 생성
- [ ] MakeSQL: 일일 리포트 생성 (Telegram + Notion)
- [ ] `abledb trading` CLI 명령 구현

### Phase 5: 실전 투입 (2주)

- [ ] 페이퍼 트레이딩 2주간 운영 → 성과 검증
- [ ] 리스크 파라미터 튜닝
- [ ] 실전 전환 (소액 시작 → 점진적 증액)

---

## 9. 리스크 관리 체계

### 다단계 안전장치

```
Level 1: 전략 엔진 (restg/rstudio/pyForMe)
  → confidence 점수 기반 필터링 (임계값 이하 신호 폐기)

Level 2: 증권사 컨테이너 (kis2/ls/upbit)
  → 종목당 최대 포지션, 일일 손실 한도, 주문 빈도 제한

Level 3: MakeSQL 컨트롤 타워
  → 전체 포트폴리오 노출 한도, 긴급 전체 중지

Level 4: 사람 (Telegram 알림)
  → 손실 임계값 초과 시 즉시 알림 → 수동 판단
```

### 자동 중지 조건

| 조건 | 임계값 | 동작 |
|---|---|---|
| 일일 총 손실 | -200만원 | 당일 전체 매매 중지 |
| 종목 손실 | -3% | 해당 종목 강제 청산 |
| 연속 손절 | 3회 연속 | 해당 전략 24시간 비활성 |
| 시스템 에러 | API 3회 연속 실패 | 해당 브로커 매매 중지 + 알림 |

---

## 10. 알려진 이슈 및 선행 조건

| 이슈 | 대상 | 심각도 | 비고 |
|---|---|---|---|
| go build 에러 | upbit_claude | 높음 | console 패키지 의존성 해결 필요 |
| 순차처리 병목 | saver_claude | 중간 | 배치/병렬 처리 전환 필요 |
| DEBUG 로그 과다 | saver_claude | 낮음 | 프로덕션 레벨 조정 |
| panic 위험 | saver_claude | 높음 | 에러 핸들링 개선 |
| SQL Injection | dbsender_claude | 높음 | 파라미터화 쿼리 전환 |
| 평문 암호 | dbsender_claude | 높음 | AES-256 암호화 적용 |

---

## 11. 참고 자료

- `docs/docker_claude_project_analysis.md` — 전체 프로젝트 분석
- `docs/guide/docker_claude_token_refresh.md` — 토큰 갱신 설정
- `docs/myAbility.md` — MakeSQL Claude 역량
- `claude_project.yaml` — 로컬 프로젝트 경로 매핑
- `config.yaml` — DB 연결 정보 (암호화)
