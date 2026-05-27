# Docker Claude 토큰 자동 갱신 가이드

> 수정일: 2026-05-27

## 문제

Docker Claude 컨테이너에서 하루 2~3회 `/login`을 요구하는 현상.

## 원인

`docker-compose.yml`에서 `credentials.json`을 **`:ro` (읽기전용)**으로 마운트하고 있었음.

```yaml
# 수정 전 (문제)
- /home/feihong/.claude/.credentials.json:/home/node/.claude/.credentials.json:ro
```

- Claude Code OAuth 토큰 수명: **8시간**
- `refreshToken`이 존재하므로 자동 갱신이 가능하지만, `:ro` 마운트 때문에 갱신된 토큰을 파일에 저장할 수 없었음
- 8시간 후 토큰 만료 → API 호출 실패 → `/login` 필요

## 수정

```yaml
# 수정 후 (정상)
- /home/feihong/.claude/.credentials.json:/home/node/.claude/.credentials.json
```

- 15개 Docker Claude의 `docker-compose.yml`에서 `:ro` 제거
- 컨테이너 내부에서 `refreshToken`으로 토큰 자동 갱신 가능

## 수정 후 동작

- 컨테이너 내부 Claude Code가 토큰 만료 전에 `refreshToken`으로 자동 갱신
- 갱신된 토큰이 `credentials.json`에 저장됨 (bind mount이므로 호스트 파일도 갱신)
- `/login` 빈도 대폭 감소

## 여전히 /login이 필요한 경우

- 호스트에서 `/login` 실행 시 `credentials.json` 파일이 **새로 생성**(inode 변경)
- Docker bind mount는 inode를 추적하므로, 파일이 교체되면 컨테이너가 기존 inode를 참조
- 이 경우 Docker 컨테이너 재시작 필요 (`docker compose restart`)

## 참고

| 항목 | 값 |
|---|---|
| subscriptionType | max |
| rateLimitTier | default_claude_max_20x |
| accessToken 수명 | 약 8시간 |
| refreshToken | 존재 (자동 갱신용) |
| Claude Code 버전 | 2.1.81+ |
