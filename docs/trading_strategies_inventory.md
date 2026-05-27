# 전체 매매 전략 인벤토리

> 작성일: 2026-05-27
> 분석 대상: 6개 프로젝트, 37개 전략

---

## 전략 요약

| 프로젝트 | 언어 | 시장 | 전략 수 | 보유기간 | 핵심 기법 |
|---|---|---|---|---|---|
| RESTG | C# | 국내 주식 | 13 매수 + 5단계 매도 | 시간~일 | 박스 돌파 |
| KIS2 | Go | 국내 주식 | 5개 퀀트 | 일~월 | 수급/팩터/이벤트 |
| python_ForMe | Python | 글로벌 주식 | 8개 백테스트 | 일~월 | SMA/RSI/CAPM/FF |
| Upbit | Go | 암호화폐 | 2개 기술적 | 시간~일 | MA/RSI |
| StockTopReason | Go | 국내 주식 | 2개 이벤트/AI | 실시간 | 이상 탐지 |
| RStudio | R | 글로벌 자산 | 7개 배분 | 월 | 모멘텀/리스크패리티 |

---

## 1. RESTG (C#) — 국내 주식 박스 돌파 전략

### 1-1. 매수 전략 (13개)

RESTG의 매수 전략은 **DefBox**(방어 박스)와 **MainBox**(메인 박스)라는 가격 구조 분석에 기반한다. 주가가 DefBox 상단을 돌파할 때 매수 신호를 생성한다.

#### REST1 통합 매수 (9개)

| # | 전략명 | DefCount | 핵심 조건 | 신호 유형 |
|---|---|---|---|---|
| 1 | SingleOrMultiDefBuy_Option1 | 1 또는 ≥2 | DefBox 근접 7%, MA60>MA120 2%+ | 즉시 매수 |
| 3 | SingleDefStrictBuy_Option2 | 1 | 엄격 타이밍 검증, MA20 터치, MA 배열 | 즉시 매수 |
| 4 | SingleDefWeakFoundationBuy_Option2 | 1 | 완화 거리, MA 조건 없음, 저리스크 | 즉시 매수 |
| 8 | SingleDefRelaxedDistanceBuy_Option3 | 1 | MainBox 거리 ≥2x, 박스수 ≤5 | 즉시 매수 |
| 23 | Intersection_4n8 | 1 | 전략 4∩8 복합 조건 | 즉시 매수 |
| 5 | MultiDefStandardBuy_Option2 | ≥2 | MA60>MA120 2%, DamCount ≤2 | 즉시 매수(MD) |
| 6 | MultiDefRelaxedBuy_Option2 | ≥2 | DamCount 2~5, 유연한 MA 조건 | 확인 대기 |
| 10 | MultiDefStandardBuy_Option3 | ≥2 | 전략5 + 관통 조건 제외 | 즉시 매수 |
| 11 | MultiDefRelaxedBuy_Option3 | ≥2 | 전략6 + 관통 조건 제외 | 즉시 매수 |

#### REST2 매수 (4개)

| # | 전략명 | DefCount | 핵심 조건 | 신호 유형 |
|---|---|---|---|---|
| 13 | SingleDefBoxImmediateBuy_REST2 | 1 | MA20≈MA60 배열, DefBox 근접 7%+15% | 후보군1 대기 |
| 14 | SingleDefBoxWeakFoundationBuy_REST2 | 1 | MA 조건 없음, MainBox 거리 ≥2x | 약기반 매수 |
| 15 | MultiDefBoxWithPenetration_REST2 | ≥2 | 돌파수 ≤2, MA20≈MA60 | 즉시 매수(MD) |
| 16 | MultiDefBoxAlternative_REST2 | ≥2 | 관통수 제한 없음 | 후보군1 대기 |

#### 공통 파라미터

- DefBox 근접 판단: 현재가 대비 7% 이내
- MainBox 거리: 현재가 대비 상대적 위치
- MA 조건: MA5, MA20, MA60, MA120 이동평균 배열
- 관통(Penetration): 가격이 박스를 뚫고 지나간 횟수
- DamCount: 저항선 개수

### 1-2. 매도 전략 (5단계 다경로 시스템)

#### Phase 1: 적응형 리스크 관리

| 평가기 | 로직 |
|---|---|
| AdaptiveStopLossEvaluator | 변동성 기반 동적 손절 (-10% ~ -20%) |
| TimeDelayedStopLossEvaluator | 3일 연속 손실 확인 후 실행 |

#### Phase 2: 치명적 실패 감지 → 즉시 100% 청산

| 조건 | 기준 |
|---|---|
| 일일 급락 | -10% 이상 |
| 패닉 매도 | 거래량 2x 평균 + 하락 5%+ |
| MA 역전 | 3일 이상 지속 |
| 누적 하락 | 5일간 -15% 이상 |

#### Phase 3: 복합 신호 평가

- 다중 신호 강도 분석 (임계값 > 0.25)
- 회복 가능성 평가 (HIGH / MEDIUM / LOW)
- 가중치 매도: 50% 부분 매도 ~ 100% 전량 매도

#### Phase 4: 기술적 매도 신호 (11가지)

| 평가기 | 조건 |
|---|---|
| ProfitTakingEvaluator | +15% 익절 (50% 포지션) |
| LossCuttingEvaluator | -3~5% 고정 손절 |
| MainBoxBreakdownEvaluator | MainBox 하향 이탈 (종가 기준) |
| TechnicalSellEvaluator | MA5-MA20 데드크로스 |
| MAReversalEvaluator | MA 역전 박스 패턴 |
| MainBoxRecoveryFailEvaluator | MainBox 회복 실패 |
| EarlyWarningEvaluator | 3일 내 -7% 조기 하락 |
| BBVolatilityEvaluator | 볼린저밴드 변동성 확대 |
| RecoveryPotentialEvaluator | 회복 가능성 평가 |
| HoldingExtensionEvaluator | 보유기간 연장 판단 (최대 20일) |

#### Phase 5: 보유기간 만료

- 20일 최대 보유 후 자동 청산
- 시간 가중 퇴출 관리

---

## 2. KIS2 (Go) — 국내 주식 퀀트 전략

### 전략 1: 기관 수급 합의 추종

- **유형**: 수급 추종 (Institutional Consensus Flow)
- **유니버스**: KOSPI + KOSDAQ 시총 상위 500 (350~400 종목)
- **진입 조건**:
  - 외국인 3일 연속 순매수
  - 펀드 또는 투신 3일 중 2일 순매수
  - 개인 3일 중 2일 순매도
  - 현재가 > 20일 이동평균
- **보너스 점수**: 펀드+투신 동시 매수(+4), 신규 펀드(+2), 대규모 외인 거래(+2), 5일 패턴(+3)
- **퇴출**: +8%(50%), +15%(전량), -5% 손절, 20일 최대
- **기대 성과**: 승률 55~60%, 손익비 1.8:1, 연 10~18%
- **사용 API**: KIS 12개 수급 API

### 전략 2: 멀티팩터 가치/성장 하이브리드

- **유형**: 팩터 기반 (Value + Growth + Quality)
- **유니버스**: 시총 1,000억+ (500~600 종목)
- **스코어링** (90점 만점):
  - 가치 (30점): PER, EV/EBITDA, PSR 섹터 내 백분위
  - 성장 (40점): 매출 성장률, 영업이익 성장률, ROE, 자산 성장률
  - 안정성 (20점): 부채비율, 유보율
- **진입 기준**: 63점 이상 (상위 15~20%)
- **보너스**: 외인 5일 순매수(+10), 배당률 2%+(+5), 애널리스트 BUY(+5), 6개월 모멘텀(+5)
- **리밸런싱**: 분기별 (실적 발표 후)
- **기대 성과**: 승률 50~55%, 연 12~18%

### 전략 3: 프로그램 수급 & 섹터 로테이션

- **유형**: 시장 미시구조 (Program Trading Signal)
- **유니버스**: KOSPI 섹터 지수 (~30개 섹터)
- **핵심 인사이트**: 비차익 프로그램 매매 = 기관 전략적 의도 반영
- **진입**: 비차익 프로그램 3일 연속 순매수 섹터
- **퇴출**: 프로그램 2일 연속 순매도 전환 시
- **보유기간**: 3~10 영업일
- **실행**: 섹터 ETF 바스켓 매매

### 전략 4: KSD 이벤트 캘린더 알파 (3개 하위 전략)

**Sub-A: 배당 캡처**
- 배당수익률 ≥3%, 3년 연속 배당 이력
- 배당 기준일 전 진입 → 배당락일 후 퇴출

**Sub-B: 유상증자 반전**
- 할인율 ≥20%, 현재가가 증자가 대비 -10% 이하
- 권리락일 전 진입 → 권리 소멸일 후 퇴출

**Sub-C: 합병 차익**
- 확정 합병 딜, 합병비율 대비 가격 괴리
- 공시 후 진입 → 합병 완료 시 퇴출

- **기대 수익**: 이벤트당 2~8%
- **보유기간**: 이벤트에 따라 1~90일

### 전략 5: 공매도 숏스퀴즈 역발상

- **유형**: 역발상 (Short Covering Reversal)
- **유니버스**: 공매도 상위 종목 50~80개
- **진입 조건**:
  - 공매도 비율 6개월 이동 백분위 상위 5%
  - 공매도 잔고 일간 -2% 이상 감소 시작
  - 긍정적 뉴스 촉매
  - 애널리스트 매도 의견 없음
- **퇴출**: 공매도 비율 중위값 하락 (커버링 완료) 또는 +20%
- **제외**: 실적 -20% 이상 감소, 경영 이슈 종목
- **기대 수익**: 거래당 12~18%

---

## 3. python_ForMe (Python) — 글로벌 주식 백테스트 전략

### 기술적 전략 (6개)

| 전략 | 유형 | 핵심 파라미터 | 진입/퇴출 |
|---|---|---|---|
| SMA Timing | 추세 추종 | SMA 기간 커스텀 | 가격 > SMA → 매수, < SMA → 매도 |
| SMA Comparison | 다중 SMA 랭킹 | 여러 SMA 기간 비교 | 상위 자산 롱, 하위 홀드 |
| SMA Crossover | MA 교차 | 단기/장기 MA | 골든크로스 매수, 데드크로스 매도 |
| RSI Mean Reversion | 평균회귀 | RSI 14, 30/70 | RSI<30 매수, RSI>70 매도 |
| Bollinger Bands | 변동성 기반 | 20일 MA, 2σ | 하단 터치 매수, 상단 터치 매도 |

### 자산배분 전략 (3개)

| 전략 | 유형 | 설명 |
|---|---|---|
| Equal Weight | 패시브 | 동일 비중 배분, 정기 리밸런싱 |
| All-Weather | 멀티자산 | 주식/채권/원자재, 모든 시장 환경 대응 |
| GDAA | 동적 배분 | 모멘텀 기반 레짐 스위칭 |

### 팩터 분석 모듈

| 모듈 | 설명 |
|---|---|
| CAPM 분석 | 개별 종목 vs 시장 회귀 분석 |
| 가치 팩터 | PBR, PER, PCR 랭킹 (Fama-French) |
| 모멘텀 팩터 | 12개월 수익률 기반 포트폴리오 |
| 수익성 팩터 | 영업이익/자산(OP) 포트폴리오 |
| 가치 포트폴리오 | PER+PBR 복합 랭킹 (국내 상위 20종목) |
| 모멘텀 포트폴리오 | 6개월 수익률 랭킹 (국내 상위 20종목) |
| 퀄리티 포트폴리오 | ROE/부채비율 복합 랭킹 |

**시장**: 글로벌 주식 (Yahoo Finance) + 국내 주식
**프레임워크**: bt 라이브러리

---

## 4. Upbit (Go) — 암호화폐 기술적 전략

### MA 크로스오버 전략

- **유형**: 추세 추종
- **기본 파라미터**: 단기 MA 5 / 장기 MA 20 / 60분봉
- **매수**: 단기 MA가 장기 MA 상향 돌파
- **매도**: 단기 MA가 장기 MA 하향 돌파

### RSI 평균회귀 전략

- **유형**: 오실레이터 기반 평균회귀
- **기본 파라미터**: RSI 14일 / 과매수 70 / 과매도 30
- **매수**: RSI < 30
- **매도**: RSI > 70

### 복합 신호 실행

- MA와 RSI가 **동시에 일치**해야 실제 주문 실행
- 시장별 예산: 100,000 KRW (설정 가능)
- 체크 간격: 60초
- 캔들 이력: 200개

---

## 5. StockTopReason (Go) — 이벤트/AI 분석

### 급등 종목 탐지

- **기준**: 일일 25%+ 급등
- **분석 기간**: 100일 lookback
- **데이터**: KIS MSSQL DB (BP_PeriodPrice)
- **산출물**: Telegram 알림 + 급등 원인 분석

### 급락 종목 탐지

- **기준**: 일일 15%+ 급락
- **분석 기간**: 100일 lookback
- **산출물**: Telegram 알림 + LLM 기반 원인 분석 (AI)

**활용**: 실시간 감시 + 전략 엔진(RESTG)에 이벤트 피드

---

## 6. RStudio (R) — 글로벌 자산배분

### 7개 백테스트 엔진

| 엔진 | 유형 | 설명 |
|---|---|---|
| back_base | 기본 | 가격 로딩, 수익률 계산, 리밸런싱 스케줄러 |
| back_composite | 전술적 멀티자산 | **2-Layer 복합 전략** (핵심 엔진) |
| back_gdaa | 동적 자산배분 | 모멘텀 기반 레짐 스위칭 (60/126/252일) |
| back_kr5asset | 국내 5자산 | KOSPI 중심 국내 배분 |
| back_maxdiv | 최대 분산 | 변동성 역가중, 분산비율 최대화 |
| back_minvar | 최소 분산 | 포트폴리오 변동성 최소화 (QP) |
| back_riskparity | 리스크 패리티 | 동일 리스크 기여도 배분 |

### 2-Layer 복합 전략 (back_composite) 상세

**Layer 1 — 시장 레짐 판단 (5개 규칙)**

| 규칙 | 조건 | 판단 |
|---|---|---|
| SPY 멀티모멘텀 | 3/6/12개월 모멘텀 | 미국 주식 강세/약세 |
| 미국 주식 약세 | T10Y2Y < 0.3 (수익률 곡선 역전) | 경기 침체 신호 |
| GLD 멀티모멘텀 | 1/3/6개월 모멘텀 | 금 강세/약세 |
| TLT 장기채 모멘텀 | 멀티모멘텀 | 채권 강세/약세 |

**Layer 2 — 자산군별 모멘텀 셀렉션 (Sleeve)**

| Sleeve | 후보 자산 | 선택 방식 |
|---|---|---|
| 미국 주식 | SPY, QQQ, SMH, ITA | 모멘텀 상위 2개 |
| 한국 지수 | 069500.KS, 091160.KS, 305720.KS, 455030.KS | 모멘텀 상위 2개 |
| 금/원자재 | GLD, GDX, SLV | 최소분산 포트폴리오 |

**기본 배분**: 미국 40% + 한국 40% + 이머징 5% + 금 5% + 채권 10%

### 24개 YAML 설정 파일

**전술적 (11개)**
- tactical_composite_v2 — 5규칙 3슬리브 (기본)
- tactical_composite_v2_4040 — 미국/한국 40/40
- tactical_composite_v2_4040_bond — 채권 강화
- tactical_composite_v2_4040_defend — 방어적
- tactical_ma / tactical_ma_sleeve / sleeve2 / sleeve3 — MA 기반 변형
- tactical_maxdiv_sleeve / sleeve2 — 최대분산 변형
- tactical_composite_sleeve2 — 복합 슬리브 변형

**정적/균형 (6개)**
- balanced / balanced2 / static_balanced / static_balanced2
- static_balanced_sleeve / cache_sleeve

**룰 기반 (4개)**
- rules1 / rules1_sleeve / rules2 / rules2_sleeve

---

## 전략 유형별 분류

### 추세 추종 (Trend Following)

| 전략 | 프로젝트 | 시장 |
|---|---|---|
| MA 크로스오버 | Upbit, pyForMe | 암호화폐, 글로벌 |
| SMA Timing/Comparison | pyForMe | 글로벌 |
| SPY/GLD 멀티모멘텀 | RStudio | 글로벌 |
| GDAA | pyForMe, RStudio | 글로벌 |

### 박스 돌파 (Breakout)

| 전략 | 프로젝트 | 시장 |
|---|---|---|
| DefBox/MainBox 13종 | RESTG | 국내 주식 |

### 평균회귀 (Mean Reversion)

| 전략 | 프로젝트 | 시장 |
|---|---|---|
| RSI 과매수/과매도 | Upbit, pyForMe | 암호화폐, 글로벌 |
| 볼린저밴드 | pyForMe | 글로벌 |

### 팩터 기반 (Factor)

| 전략 | 프로젝트 | 시장 |
|---|---|---|
| 멀티팩터 가치/성장 | KIS2 | 국내 주식 |
| Fama-French 팩터 | pyForMe | 글로벌/국내 |

### 수급 기반 (Flow)

| 전략 | 프로젝트 | 시장 |
|---|---|---|
| 기관 수급 합의 | KIS2 | 국내 주식 |
| 프로그램 수급/섹터 로테이션 | KIS2 | 국내 주식 |

### 이벤트 드리븐 (Event-Driven)

| 전략 | 프로젝트 | 시장 |
|---|---|---|
| KSD 이벤트 (배당/증자/합병) | KIS2 | 국내 주식 |
| 급등/급락 AI 분석 | StockTopReason | 국내 주식 |
| 공매도 숏스퀴즈 | KIS2 | 국내 주식 |

### 자산배분 (Asset Allocation)

| 전략 | 프로젝트 | 시장 |
|---|---|---|
| All-Weather / Equal Weight | pyForMe | 글로벌 |
| 2-Layer 복합전략 | RStudio | 글로벌 |
| MinVar / MaxDiv / RiskParity | RStudio | 글로벌 |
| Korean 5-Asset | RStudio | 국내 |

---

## 추가 개발 가능 전략 방향

### 1. 크로스 프로젝트 복합 전략

- RESTG 박스 돌파 신호 + KIS2 수급 필터 → 수급 확인된 돌파만 매수
- StockTopReason AI 분석 + RESTG 기술적 필터 → 급등 후 구조적 매수 타이밍
- RStudio 자산배분 + pyForMe 팩터 스코어 → 팩터 가중 자산배분

### 2. 미구현 전략 영역

- **페어 트레이딩**: 상관관계 높은 종목 간 스프레드 매매
- **VWAP/TWAP**: 대량 주문 분할 실행 알고리즘
- **옵션 전략**: 커버드콜, 프로텍티브풋 (LS증권 옵션 API 활용)
- **뉴스 센티먼트**: dart_claude 공시 + StockTopReason AI → 실시간 센티먼트 신호
- **변동성 매매**: VIX 등가물 활용한 변동성 레짐 전략

### 3. 암호화폐 확장

- Upbit MA/RSI 외 추가: MACD, 볼린저밴드, 온체인 지표
- 다중 거래소 차익거래 (향후 거래소 API 추가 시)
- 김치 프리미엄 모니터링 전략
