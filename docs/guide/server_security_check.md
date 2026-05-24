# 서버 보안 점검 가이드

> 작성일: 2026-05-24
> 대상 서버: white (192.168.3.120 / 100.68.156.109)

---

## 점검 절차

### 1️⃣ 현재 외부로 나가는 연결 확인

```bash
ss -tunp | grep ESTAB
```

**확인 포인트:**
- 프로세스 이름이 표시되지 않는 연결 → `sudo ss -tunp`로 재확인 (root 권한 프로세스)
- 알 수 없는 외부 IP로의 연결 여부
- 정상 프로세스 목록: `logger_Hope`, `News_Hope`, `bun`(Telegram), `tailscaled`, `KIS`, `abledb_Hope`, `TopReason_Hope`

---

### 2️⃣ 수상한 프로세스 확인

```bash
ps aux --sort=-%cpu | head -30
```

**확인 포인트:**
- CPU 사용률이 비정상적으로 높은 프로세스
- 알 수 없는 프로세스명
- 오래된 좀비 프로세스 (START 날짜 대비 TIME이 과도하게 큰 것)

**정상 프로세스 목록:**

| 프로세스 | 설명 |
|---|---|
| `logger_Hope` | MongoDB/MSSQL 로깅 |
| `News_Hope` | 뉴스 수집 스케줄러 |
| `bun server.ts` | Telegram 봇 (여러 인스턴스) |
| `KIS` | KIS 스케줄러 |
| `TopReason_Hope` | AI 분석 스케줄러 |
| `abledb_Hope` | abledb 스케줄러 |
| `tailscaled` | Tailscale VPN 데몬 |
| `mongod` | MongoDB (2개 인스턴스) |
| `sqlservr` | MSSQL Server |
| `redis-server` | Redis |
| `mysqld` | MySQL |
| `beam.smp` | RabbitMQ (Erlang) |
| `ora_dia0_ORCLCDB` | Oracle DB |
| `claude` | Claude Code (작업 중인 것들) |

---

### 3️⃣ 최근 로그인 기록 (외부 침입 여부)

```bash
last -20
sudo lastb | head -20
```

**확인 포인트:**
- 허가되지 않은 IP에서의 로그인
- `lastb`에 브루트포스 시도 다수 여부
- 허용된 접속 IP: `100.78.65.25`, `100.82.67.31` (Tailscale), `192.168.3.x` (내부망)

---

### 4️⃣ 크론잡 확인 (자동 실행 스크립트)

```bash
crontab -l
sudo crontab -l
ls /etc/cron* /var/spool/cron/
```

**정상 크론잡:**
```
17 * * * * python3 /home/feihong/code/blog/main.py post sync   # 블로그 자동 동기화
```

**시스템 cron (`/etc/cron.d`):** Ubuntu 기본 패키지만 존재
- `e2scrub_all`, `popularity-contest`, `sysstat` → 정상

---

### 5️⃣ 최근 변경된 파일 확인 (악성 스크립트)

```bash
find /tmp /var/tmp /home -type f -newer /etc/passwd 2>/dev/null
```

**필터링된 점검 (개발 도구 제외):**
```bash
find /tmp /var/tmp -type f -newer /etc/passwd 2>/dev/null \
  | grep -v -E '(\.vscode-server|\.bun|\.cache|\.local|__pycache__|\.npm|anaconda3|\.config|node_modules|\.nuget|\.dotnet)' \
  | head -60
```

**실행 권한 있는 의심 파일 확인:**
```bash
find /tmp /var/tmp -type f -newer /etc/passwd -perm /111 2>/dev/null
```

**정상으로 확인된 /tmp 파일 유형:**
- Python 스크립트 (.py) — 블로그/Notion 작업용
- `notion_images/` — Notion 이미지 캐시
- `Roslyn.Intellisense/` — C# VS Code 언어서버 임시파일
- `yt_oauth.log`, `yt_oauth_pid` — YouTube OAuth

---

## 점검 결과 기록 (2026-05-24)

| 항목 | 결과 |
|---|---|
| 외부 연결 | ✅ 정상 (tailscaled, bun/Telegram, Hope 시스템) |
| 프로세스 | ✅ 정상 (좀비 프로세스 1개 제거: PID 131088 .NET Interactive) |
| 로그인 기록 | ✅ 정상 (침입 없음, btmp 실패 기록 없음) |
| 크론잡 | ✅ 정상 |
| /tmp 파일 | ✅ 정상 (실행 권한 있는 의심 파일 없음) |

### 조치 사항
- `kill -9 131088` — `.NET Interactive` 프로세스 37일째 CPU 100% 점유, 제거 완료
- nginx `blocklist.conf`에 악성 IP 17개 추가 (2026-05-24)

---

## nginx IP 차단 방법

```bash
# 원격 서버 접속
ssh -i /home/feihong/code/MakeSQL/moodle.pem ubuntu@3.34.223.162

# IP 추가
echo 'deny <IP주소>;  # 사유 (날짜)' | sudo tee -a /etc/nginx/conf.d/blocklist.conf

# 적용
sudo nginx -t && sudo systemctl reload nginx
```
