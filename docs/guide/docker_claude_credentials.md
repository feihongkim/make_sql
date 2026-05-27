# Docker Claude 컨테이너 credentials 문제

## 증상

호스트에서 `/login`을 완료해도 Docker Claude 컨테이너들이 계속 `/login`을 요구하며,
컨테이너 내부에서 `/login`을 해도 다시 요구하는 루프 발생.

## 원인

### Docker bind mount의 inode 문제

`docker-compose.yml` 볼륨 설정:
```yaml
volumes:
  - /home/feihong/.claude/.credentials.json:/home/node/.claude/.credentials.json:ro
  - kis2_claude_data:/home/node/.claude
```

1. Claude Code가 `/login` 시 `credentials.json`을 **in-place 수정이 아닌 새 파일(새 inode)로 교체**
2. Docker bind mount는 **inode 레벨**로 동작
3. 호스트가 새 inode로 파일을 교체하면, **컨테이너는 삭제된 OLD inode를 계속 참조**
4. 컨테이너 내부 파일은 `:ro` (읽기전용)이라 컨테이너 자체 로그인으로 덮어쓰기 불가 → 루프

### Named Volume shadowing 문제

named volume(`kis2_claude_data:/home/node/.claude`)이 `.claude` 디렉토리 전체를 마운트하면서
credentials 파일의 bind mount를 shadow할 수 있음.

## 해결 (즉시)

**호스트 `/login` 후 모든 Docker Claude 컨테이너 재시작**:

```bash
for container in $(docker ps --format "{{.Names}}" | grep _claude); do
    docker restart "$container"
done
docker restart claude_python_forme
```

재시작 시 Docker가 새 inode로 bind mount를 재연결하여 최신 credentials 적용됨.

## 재시작 확인

```bash
docker ps --format "table {{.Names}}\t{{.Status}}" | grep claude
```

## 근본 대책 (권장)

호스트에서 `/login`을 수행한 이후에는 **반드시 모든 Docker Claude 컨테이너를 재시작**해야 함.

필요 시 자동화 스크립트 추가 가능:
```bash
# ~/code/DockerClaude/restart_all_claude.sh
#!/bin/bash
for container in $(docker ps --format "{{.Names}}" | grep _claude); do
    docker restart "$container"
    echo "Restarted: $container"
done
docker restart claude_python_forme 2>/dev/null || true
```
