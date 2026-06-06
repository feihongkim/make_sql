# Jarvis Docs Site — 운영 인수인계

> 최초 구축: 2026-06-06
> 인수인계 대상: 시스템 관리자 claude
> 본 문서는 Jarvis 프로젝트 md 문서 게시 사이트(mkdocs)의 모든 운영 정보를 담는다.

---

## 1. 개요

Jarvis 프로젝트의 모든 md 문서를 폴더 구조 그대로 웹으로 게시하는 정적 사이트.

- **무엇**: mkdocs Material 테마 기반 정적 사이트
- **왜**: 트레이딩 전략·분석 문서를 폴더 구조 그대로 웹에서 열람 + 검색
- **누가**: 현재 본인 only (외부 공개 X)
- **호스트**: white 서버 (192.168.3.120)

## 2. 접속 URL

| 환경 | URL |
|---|---|
| LAN 내부 | http://192.168.3.120:8000 |
| Tailscale | http://100.68.156.109:8000 또는 http://white:8000 |

## 3. 구성 요소

### 3-1. `mkdocs_claude` 컨테이너

- **이미지**: `squidfunk/mkdocs-material:latest`
- **포트**: `8000:8000`
- **마운트**:
  - `/home/feihong/code/Jarvis` → `/site/docs` (메인 콘텐츠)
  - `/home/feihong/code/Jarvis/mkdocs.yml` → `/site/mkdocs.yml:ro` (설정)
- **환경변수**: `WATCHDOG_FORCE_POLLING=true` (mkdocs serve가 실제로 무시하지만 향후 참고용으로 남김)
- **명령**: `serve --dev-addr=0.0.0.0:8000`
- **재시작 정책**: `unless-stopped`
- **working_dir**: `/site`

### 3-2. 호스트 polling watcher (`mkdocs-watch.service`)

- **systemd 서비스**: `mkdocs-watch.service`, enabled + active
- **스크립트**: `/home/feihong/code/DockerClaude/Mkdocs/watch.sh`
- **동작**: 1초 polling, 2초 debounce, `*.md` / `*.yml` / `*.css` 감시
- **트리거 시**: `docker restart mkdocs_claude` (약 0.5초)
- **존재 이유**: mkdocs serve의 LiveReload가 alpine 컨테이너 + bind mount에서 호스트 파일 변경을 감지 못함. 호스트 polling으로 우회.

### 3-3. mkdocs 설정 파일

| 파일 | 역할 |
|---|---|
| `/home/feihong/code/Jarvis/mkdocs.yml` | site_name, theme, plugins, extra_css 등 |
| `/home/feihong/code/Jarvis/stylesheets/extra.css` | 다크/라이트 모드 색상, 사이드바 토글 아이콘 등 |
| `/home/feihong/code/Jarvis/index.md` | 홈페이지 |

## 4. 파일 위치 요약

| 항목 | 호스트 경로 |
|---|---|
| 콘텐츠 (모든 md) | `/home/feihong/code/Jarvis/` |
| 사이트 설정 | `/home/feihong/code/Jarvis/mkdocs.yml` |
| 커스텀 CSS | `/home/feihong/code/Jarvis/stylesheets/extra.css` |
| docker-compose.yml | `/home/feihong/code/DockerClaude/Mkdocs/docker-compose.yml` |
| watcher 스크립트 | `/home/feihong/code/DockerClaude/Mkdocs/watch.sh` |
| systemd unit (활성) | `/etc/systemd/system/mkdocs-watch.service` |
| systemd unit (백업본) | `/home/feihong/code/DockerClaude/Mkdocs/mkdocs-watch.service` |
| 컨테이너 작업환경 | `/home/feihong/code/DockerClaude/Mkdocs/` |

## 5. 운영 명령

### 5-1. 사이트 (mkdocs_claude)

```bash
# 시작
cd /home/feihong/code/DockerClaude/Mkdocs && docker compose up -d

# 중지
cd /home/feihong/code/DockerClaude/Mkdocs && docker compose down

# 재시작 (가장 자주 쓰는 명령)
docker restart mkdocs_claude

# 상태
docker ps --filter name=mkdocs_claude

# 로그
docker logs mkdocs_claude --tail 50

# 실시간 로그
docker logs mkdocs_claude -f
```

### 5-2. Watcher (systemd)

```bash
# 상태
systemctl status mkdocs-watch.service

# 재시작 (watch.sh 수정 후 필수)
sudo systemctl restart mkdocs-watch.service

# 로그
journalctl -u mkdocs-watch.service -n 50

# 실시간 로그
journalctl -u mkdocs-watch.service -f

# 비활성화
sudo systemctl disable --now mkdocs-watch.service
```

## 6. 동작 흐름

```
1. 사용자 → /home/feihong/code/Jarvis 안 md 파일 수정 + 저장
2. watch.sh (1초 polling) → 최근 mtime 변경 감지
3. 마지막 수정 후 2초 안정 → docker restart mkdocs_claude
4. mkdocs serve → 모든 콘텐츠 재빌드 (1~2초)
5. 브라우저 새로고침 → 변경 반영
```

총 지연: 약 2~4초.

## 7. 트러블슈팅

### 7-1. 사이트가 안 보임

```bash
# 1) 컨테이너 동작 확인
docker ps --filter name=mkdocs_claude

# 2) 에러 로그
docker logs mkdocs_claude --tail 30

# 3) 호스트에서 응답 확인
curl http://localhost:8000/

# 4) 외부 접근 불가
#    - LAN: 같은 네트워크 확인 (192.168.3.x)
#    - Tailscale: tailscale status로 white 노드 연결 확인
```

### 7-2. md 수정해도 반영 안 됨

```bash
# 1) Watcher 동작 확인
systemctl status mkdocs-watch.service

# 2) Watcher 로그 확인
journalctl -u mkdocs-watch.service --since "5 minutes ago"

# 3) 수동 강제 재시작
docker restart mkdocs_claude
```

### 7-3. mkdocs 빌드 에러

- `docker logs mkdocs_claude --tail 50`에서 `ERROR` 라인 확인
- 주요 원인:
  - `mkdocs.yml` 문법 오류
  - md 파일 frontmatter 오류
  - extra_css 경로 오류
- 컨테이너는 mkdocs가 에러 나도 실행은 유지 (이전 빌드 결과 서빙)

### 7-4. watch.sh 수정 후 적용 안 됨

`watch.sh`는 systemd가 실행 중이라 메모리에 옛 코드 보유. 반드시:

```bash
sudo systemctl restart mkdocs-watch.service
```

### 7-5. 포트 충돌

호스트 8000번 포트를 다른 서비스가 점유한 경우 `docker-compose.yml`에서 `"18000:8000"`처럼 우회. (현재는 8000 비어 있음 확인됨.)

## 8. 설계 결정 (Why)

### 8-1. 왜 jarvis_claude 안이 아니라 별도 컨테이너?

- **생애주기 분리** — jarvis_claude는 Claude Code 작업환경 (수시 재빌드/재시작), 사이트는 24/7 서비스
- **책임 분리** — 한 컨테이너에 다중 책임 두면 디버깅·로그·의존성 충돌 발생
- **다른 docker_claude들과의 일관성** — 운영 패턴 통일

### 8-2. 왜 mkdocs Material?

- Python 기반, 단일 설정 파일
- 다크모드·검색·모바일 반응형 기본 제공
- 한국어 검색 지원 (lunr.js + ko)
- 폴더 구조 자동 사이드바화 (별도 nav 명시 불필요)

### 8-3. 왜 외부 polling watcher?

mkdocs serve의 LiveReload는 watchdog 라이브러리 기반인데, 본 환경에서는:
- alpine 컨테이너 + bind mount → inotify가 호스트 변경을 컨테이너로 전파 못함
- `WATCHDOG_FORCE_POLLING=true` 환경변수 적용해도 mkdocs serve가 무시
- 컨테이너 내부에서 직접 수정해도 mkdocs serve가 감지 못함 (재현 확인됨)

따라서 호스트에서 mtime polling → docker restart가 가장 안정적.

### 8-4. 왜 docs_dir 마운트 트릭?

- mkdocs는 `docs_dir`이 `mkdocs.yml`의 부모 디렉토리이면 안 됨
- 호스트 `/home/feihong/code/Jarvis`에 mkdocs.yml과 md 파일이 같이 있음
- 해결: 컨테이너 안에서 `/site/docs` (콘텐츠) + `/site/mkdocs.yml` (설정)로 별도 마운트 → 부모-자식 관계 형성

## 9. 변경 이력

| 날짜 | 변경 |
|---|---|
| 2026-06-06 | 초기 구축 — mkdocs_claude + watcher + Material 테마 |
| 2026-06-06 | UI — 다크모드 기본 / 좌측 사이드바 / 폴더 토글 / 색상 통일 |

## 10. 알려진 한계

- mkdocs serve의 LiveReload 직접 사용 불가 → polling 우회 (2~4초 지연)
- 정적 빌드가 아니라 in-memory serve → 동시접속 많아지면 성능 우려 (현재는 본인만 사용해 무관)
- nav 순서는 알파벳 자동 (사용자 정의 nav 명시 안 함)
- watch.sh가 `*.md`/`*.yml`/`*.css`만 감시 — 이미지·JSON·기타 자산 변경은 미감지 (필요 시 확장)

## 11. 잠재 개선 항목

| 항목 | 설명 | 우선순위 |
|---|---|---|
| 외부 공개 + 인증 | Cloudflare Tunnel + Basic Auth / Tailscale Funnel | 낮음 |
| 정적 빌드 + nginx | `mkdocs build` → 볼륨 → nginx 서빙. 안정성↑ | 중간 |
| Git 커밋 hook | md 저장 시 자동 git commit (변경 이력 영구 보존) | 낮음 |
| 검색 가중치 | 트레이딩 용어 인덱싱 강화 | 낮음 |
| 빌드 시간 단축 | docs 수가 100개 넘으면 검토 | 향후 |

## 12. 인수인계 체크리스트 (시스템 관리자 claude용)

- [ ] 본 문서 전체 1회 통독
- [ ] `docker ps --filter name=mkdocs_claude`로 컨테이너 상태 확인
- [ ] `systemctl status mkdocs-watch.service`로 watcher 상태 확인
- [ ] http://192.168.3.120:8000 접속 확인
- [ ] 테스트 md 1개 수정 → 3초 내 반영되는지 확인
- [ ] `journalctl -u mkdocs-watch.service -n 20`으로 watcher 로그 정상 여부 확인
- [ ] (선택) 본인 환경에 mkdocs Material 이미지 pre-pull: `docker pull squidfunk/mkdocs-material:latest`

## 13. 긴급 연락 / 보고

- 운영 문제 발생 시 본 문서 7장(트러블슈팅) 우선 적용
- 미해결 시 사용자(feihong)에게 보고

## 14. 참고 — 컨테이너 호스트 환경

- 호스트: white (192.168.3.120)
- Docker: 29.4.0
- 다른 `docker_claude` 컨테이너 다수 운영 중 (`/home/feihong/code/DockerClaude/{프로젝트}/`)
- 본 사이트와 같은 패턴 따름
