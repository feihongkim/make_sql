# Nginx 보안 강화 가이드

> 적용 서버: `3.34.223.162` (AWS Lightsail, Ubuntu 24.04)
> 최초 적용: 2026-05-21 / 최종 업데이트: 2026-05-29
> 대상 사이트: `feivyblog.bnslab.biz` (Next.js 블로그), `tems.bnslab.biz` (Moodle LMS)

---

## 배경

nginx 접근 로그 자동 분석(MakeSQL 스케줄러, 2시간마다 실행) 결과 아래와 같은 외부 위협이 확인되어 보안 강화 조치를 시행함:

| 위협 유형 | 내용 |
|---|---|
| EXPLOIT_SCAN | `.env`, `wp-admin`, `/actuator/health`, `/manager/html` 등 취약 경로 스캔 |
| PATH_TRAVERSAL | PHP pearcmd + `../../../` 경로 순회로 `/tmp/` 웹쉘 생성 시도 |
| BRUTE_FORCE_PATH | 로그인 페이지(`/login.html`, `/login.jsp` 등) 순차 탐색 |
| 고빈도 스캔 | 단일 IP에서 수백 건 자동화 요청 |

---

## 최종 적용 설정

### 1단계 — 글로벌 설정 (`/etc/nginx/nginx.conf`)

```nginx
http {
    # Nginx 버전 노출 차단
    server_tokens off;

    # Rate Limiting zone 정의
    # mylimit: Moodle용 (5r/s, 엄격)
    limit_req_zone $binary_remote_addr zone=mylimit:10m rate=5r/s;
    # blog: feivyblog용 (10r/s, Next.js 다중 요청 허용)
    limit_req_zone $binary_remote_addr zone=blog:10m rate=10r/s;

    # ... 기존 설정 ...
}
```

**두 zone을 분리한 이유:**
Next.js 블로그는 페이지 이동 시 RSC(React Server Components) 요청(`?_rsc=`)을 병렬로 여러 개 발생시킴.
단일 zone(5r/s)으로는 정상 사용자가 Rate Limit에 걸리는 오탐이 발생하여 사이트별로 분리.

---

### 2단계 — Default Server (IP 직통 및 미등록 도메인 차단)

파일: `/etc/nginx/sites-available/default`

```nginx
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    # 응답 없이 TCP 연결 즉시 종료
    return 444;
}
```

- `return 444`: Nginx 전용 코드. 아무 응답 없이 연결 종료
- 효과: IP 직접 접근(`3.34.223.162`) 또는 임의 도메인 스캐너 차단

---

### 3단계 — 개별 가상 호스트 설정

#### feivyblog.bnslab.biz

파일: `/etc/nginx/sites-available/feivyblog.bnslab.biz`

```nginx
server {
    server_name feivyblog.bnslab.biz;

    # AI 봇 차단
    if ($http_user_agent ~* (GPTBot|ChatGPT-User|ClaudeBot)) {
        return 403;
    }

    # 보안 헤더
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header X-Content-Type-Options "nosniff" always;

    # 민감한 파일 직접 차단
    location ~* /\.(env|git|aws|ssh|htpasswd|DS_Store)$ {
        return 404;
    }
    location ~* \.(sql|bak|backup|log|key|pem)$ {
        return 404;
    }

    # Next.js 정적 파일 — Rate Limit 제외 (빌드 해시 파일, 캐싱 가능)
    location /_next/static/ {
        proxy_pass http://100.110.254.111:3000;
        proxy_set_header Host $host;
        proxy_cache_valid 200 365d;
    }

    location / {
        # blog zone 사용: 10r/s, burst=20 (RSC 병렬 요청 허용)
        limit_req zone=blog burst=20 nodelay;

        proxy_pass http://100.110.254.111:3000/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    listen 443 ssl;  # managed by Certbot
    # ... SSL 설정 ...
}
```

#### tems.bnslab.biz (Moodle)

파일: `/etc/nginx/sites-available/moodle`

```nginx
server {
    server_name tems.bnslab.biz;
    client_max_body_size 200M;

    # AI 봇 차단
    if ($http_user_agent ~* (GPTBot|ChatGPT-User|ClaudeBot)) {
        return 403;
    }

    # 보안 헤더
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header X-Content-Type-Options "nosniff" always;

    # 민감한 파일 직접 차단
    location ~* /\.(env|git|aws|ssh|htpasswd|DS_Store)$ {
        return 404;
    }
    location ~* \.(sql|bak|backup|log|key|pem)$ {
        return 404;
    }

    location / {
        # mylimit zone 사용: 5r/s, burst=5 (PHP 기반, 엄격 적용)
        limit_req zone=mylimit burst=5 nodelay;

        proxy_pass http://100.101.222.92:8080;
        proxy_http_version 1.1;
        proxy_set_header Host 100.101.222.92;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_connect_timeout 60s;
        proxy_send_timeout 300s;
        proxy_read_timeout 300s;
    }

    listen 443 ssl;  # managed by Certbot
    # ... SSL 설정 ...
}
```

**설정 요약:**

| 항목 | feivyblog | Moodle |
|---|---|---|
| Rate Limit zone | `blog` (10r/s) | `mylimit` (5r/s) |
| burst | 20 | 5 |
| `/_next/static/` 제외 | ✅ | — |
| 보안 헤더 | ✅ | ✅ |
| AI 봇 차단 | ✅ | ✅ |

---

### 4단계 — 설정 검증 및 적용

```bash
sudo nginx -t                  # 문법 검사 (반드시 sudo)
sudo systemctl reload nginx    # 무중단 설정 반영
```

---

### 5단계 — IP 차단: geo 모듈 (`geo_blocklist.conf`)

> 2026-05-25: `blockip.conf` + `blocklist.conf` 통합 (39개 IP, deny 방식)
> 2026-05-29: `blocklist.conf` → `geo_blocklist.conf`로 마이그레이션 (geo 모듈 방식)

파일: `/etc/nginx/conf.d/geo_blocklist.conf`
현재 차단: **5,614개** (개별 IP + CIDR 서브넷)

#### deny 방식 vs geo 모듈 방식

| 항목 | 기존 deny 방식 | geo 모듈 방식 (현재) |
|---|---|---|
| 설정 파일 | `blocklist.conf` | `geo_blocklist.conf` |
| 문법 | `deny 1.2.3.4;` | `1.2.3.4 1;` |
| CIDR 서브넷 | 불가 | `1.2.3.0/24 1;` (서브넷 단위 차단 가능) |
| 적용 위치 | `server` 블록 내 자동 적용 | `server` 블록에서 `if ($geo_blocked)` 명시 필요 |
| 성능 | IP당 선형 탐색 O(n) | Radix tree 기반 O(1) 조회 |
| 확장성 | 수백 개까지 적합 | 수만 개 이상 가능 |

5,000개 이상의 IP를 차단하면서 deny 방식은 성능 저하 우려가 있어 geo 모듈로 전환.

#### geo 모듈 동작 원리

```
1. nginx 시작 시 geo_blocklist.conf를 읽어 Radix tree 자료구조로 메모리에 로드
2. 모든 요청마다 $remote_addr를 트리에서 O(1)으로 조회 → $geo_blocked 변수에 0 또는 1 설정
3. 각 vhost server 블록에서 if ($geo_blocked) { return 444; } 로 차단
```

#### 설정 구조

`/etc/nginx/conf.d/geo_blocklist.conf` (nginx.conf의 http 블록에서 자동 include):
```nginx
geo $geo_blocked {
    default 0;           # 기본값: 차단 안 함
    190.2.135.111 1;     # 개별 IP 차단
    81.71.83.0/24 1;     # CIDR 서브넷 차단 (/24 = 256개 IP 일괄)
    # ... 5,614개 항목 ...
}
```

각 vhost의 server 블록 최상단:
```nginx
server {
    if ($geo_blocked) { return 444; }   # ← 응답 없이 연결 종료
    server_name feivyblog.bnslab.biz;
    # ...
}
```

> `return 444`는 HTTPS/HTTP 양쪽 server 블록 모두에 적용되어 있음.

#### 기존 blocklist.conf

```nginx
# blocklist.conf → geo_blocklist.conf로 대체됨
```

백업: `/etc/nginx/conf.d/blocklist.conf.bak`

#### IP 추가 방법

```bash
# MakeSQL 프로젝트에서 SSH 접속
ssh -i moodle.pem ubuntu@3.34.223.162

# 개별 IP 차단 추가
sudo sed -i '/^}/i\    <IP주소> 1;  # 사유 (날짜)' /etc/nginx/conf.d/geo_blocklist.conf

# CIDR 서브넷 차단 추가 (예: /24 = 256개 IP)
sudo sed -i '/^}/i\    <IP주소>/24 1;  # 사유 (날짜)' /etc/nginx/conf.d/geo_blocklist.conf

sudo nginx -t && sudo systemctl reload nginx
```

**차단 IP 판별 기준:**

| 판별 결과 | 내용 |
|---|---|
| 차단 | 저가 호스팅(WorldStream, ColoCrossing 등) + 공격 행위 확인 |
| 차단 | 중국·동유럽 IP + 등록된 hostname 없음 + 444/400 차단 이력 |
| 차단 | 위협 탐지 10건 이상 또는 스캐너 200건 이상 또는 브루트포스 30건 이상 |
| 차단 안 함 | **Shadowserver Foundation** (scan-N.shadowserver.org) — 합법적 보안 연구 기관 |
| 차단 안 함 | Google, AWS, Cloudflare 등 주요 서비스 대역 |
| 오탐 주의 | Moodle `/login/index.php` 정상 접근 → BRUTE_FORCE_PATH 오탐 가능 |
| 오탐 주의 | Next.js RSC(`?_rsc=`) 병렬 요청 → Rate Limit 오탐 가능 |

> 주의: IP 차단 전 반드시 `ipinfo.io` 등으로 소속 기관 확인. Shadowserver 등 보안 연구 기관을 잘못 차단하면 취약점 알림을 받지 못할 수 있음.

**알려진 정상 IP (차단 금지):**

| IP | 설명 | 비고 |
|---|---|---|
| 1.215.219.228 | 사무실 (KT) | curl 헬스체크 스크립트 포함 |
| 1.215.219.230 | 사무실 (KT) | Chrome Edge 블로그 정상 방문 |
| 211.234.188.81 | 정상 사용자 | Moodle 로그인 + 블로그 방문 (BRUTE_FORCE_PATH 오탐) |
| 198.235.24.145 | Palo Alto Networks Cortex Xpanse | 합법적 보안 스캐너 |
| 103.203.59.1 | security.ipip.net | HTTP 배너 감지 봇 |

---

### 6단계 — Fail2Ban 설치 (자동 IP 차단)

```bash
sudo apt update && sudo apt install fail2ban -y
```

파일: `/etc/fail2ban/jail.local`

```ini
[nginx-limit-req]
enabled  = true
port     = http,https
filter   = nginx-limit-req
logpath  = /var/log/nginx/error.log
findtime = 600
bantime  = 600
maxretry = 5
```

```bash
sudo systemctl restart fail2ban
```

**동작 방식:**
- nginx Rate Limit에 10분 내 5회 이상 걸린 IP → iptables에서 10분간 완전 차단
- nginx 처리 전 커널 레벨 차단 → 서버 부하 최소화

---

## 방어 계층 구조

```
인터넷
  │
  ▼
[Fail2Ban] ── Rate Limit 반복 위반 IP → iptables 커널 레벨 자동 밴
  │
  ▼
[Nginx — geo_blocklist.conf] ── 5,614개 IP/서브넷 차단 (geo 모듈, O(1) 조회)
  │                               $geo_blocked=1 → 444 응답 없이 연결 종료
  ▼
[Nginx — default server] ── IP 직통 / 미등록 도메인 444 차단
  │
  ▼
[Nginx — AI 봇 차단] ── GPTBot, ChatGPT-User, ClaudeBot → 403
  │
  ▼
[Nginx — Rate Limit]
  ├─ feivyblog: blog zone (10r/s, burst=20)
  └─ Moodle:    mylimit zone (5r/s, burst=5)
  │
  ▼
[Nginx — 보안 헤더 + 민감 파일 차단] ── XSS, Clickjacking, .env/.git 등
  │
  ▼
[feivyblog (Next.js) / Moodle (PHP)]
```

---

## 자동 모니터링

MakeSQL 스케줄러가 **매 3시간 :10분**마다 원격 서버 nginx 로그를 수집하여 보안 분석 후 Telegram으로 리포트 전송.

분석 항목:
- 보안 위협 (PATH_TRAVERSAL, SQL_INJECTION, EXPLOIT_SCAN 등 8종)
- Rate Limit 위반 (실제 공격 vs Next.js RSC 오탐 분리)
- Fail2Ban 자동 차단 이력
- Default Server 444 차단 건수
- Upstream 오류 (feivyblog 포트 3000 연결 실패 등)

관련 파일:
- `nginx_log/analyze.py` — 분석 스크립트
- `nginx_log/fetch_and_analyze.sh` — SSH 로그 수집 + 분석 실행
- `srv/scheduler.go` — 스케줄 등록 (`nginx_analyze`, Every:2, AtMinute:10)

---

## 추가 권장 사항

- [x] `.env` 경로 명시적 차단 — 적용 완료 (2026-05-21)
- [ ] HSTS 헤더 추가: `add_header Strict-Transport-Security "max-age=31536000" always;`
- [ ] `Content-Security-Policy` 헤더 추가 (XSS 방어 강화)
- [ ] Fail2Ban bantime을 600초 → 3600초(1시간)로 강화 검토
