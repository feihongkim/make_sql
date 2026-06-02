# Docker Claude credentials 및 토큰 갱신 가이드

> docker_claude_credentials.md + docker_claude_token_refresh.md 통합

---

## 볼륨 마운트 설정 (현재 적용 중)

```yaml
volumes:
  - /home/feihong/.claude.json:/home/node/.claude.json
  - /home/feihong/.claude/.credentials.json:/home/node/.claude/.credentials.json  # :ro 없음
  - <project>_claude_data:/home/node/.claude
```

> `:ro` 제거 완료 (2026-05-27). 컨테이너 내부에서 refreshToken으로 토큰 자동 갱신 가능.

---

## OAuth 토큰 자동 갱신

| 항목 | 값 |
|---|---|
| subscriptionType | max |
| rateLimitTier | default_claude_max_20x |
| accessToken 수명 | 약 8시간 |
| refreshToken | 존재 (자동 갱신용) |

- `:ro`가 없으므로 컨테이너 내부 Claude Code가 토큰 만료 전 자동 갱신
- 갱신된 토큰이 bind mount를 통해 호스트 파일도 갱신됨
- `/login` 빈도 대폭 감소

---

## 호스트 /login 후 컨테이너 재시작 필요

### 원인: inode 교체 문제

1. Claude Code `/login` 시 credentials.json을 in-place 수정이 아닌 **새 파일(새 inode)로 교체**
2. Docker bind mount는 **inode 레벨**로 동작
3. 호스트가 새 inode로 파일을 교체하면 컨테이너는 삭제된 OLD inode를 참조
4. 결과: 컨테이너가 만료된 토큰을 계속 사용 → `/login` 루프

### 해결

호스트에서 `/login` 실행 후 모든 Docker Claude 컨테이너 재시작:

```bash
for container in $(docker ps --format "{{.Names}}" | grep _claude); do
    docker restart "$container"
done
docker restart claude_python_forme
```

재시작 시 Docker가 새 inode로 bind mount를 재연결하여 최신 credentials 적용.

---

## Named Volume shadowing 주의

named volume(`<project>_claude_data:/home/node/.claude`)이 `.claude` 디렉토리 전체를 마운트하면서
credentials 파일의 bind mount를 shadow할 수 있음.

**원칙:**
- `.credentials.json` bind mount는 **파일 단위**로만 마운트
- `.claude/` 디렉토리 전체를 named volume으로 마운트할 때 파일 단위 bind mount가 우선 적용되는지 확인

---

## 문제 확인 명령

```bash
# 컨테이너 내부 credentials 파일 확인
docker exec <container_name> cat /home/node/.claude/.credentials.json | head -5

# 호스트 credentials 파일 확인
cat ~/.claude/.credentials.json | head -5

# 두 파일의 expiresAt 비교로 동기화 여부 확인
```
