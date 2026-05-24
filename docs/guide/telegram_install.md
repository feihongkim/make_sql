# Telegram Plugin 설치 가이드

## 1. Bun 설치

`claude --dangerously-skip-permissions`를 실행하고 `Bun 설치해 줘`라고 한다.

> Bun은 대충 최신 npm 같은 거라고 생각하면 됨

설치하고 난 뒤에 아래 코드 실행 (설치할 때 `~/.bashrc` 파일에 이 경로를 넣는다):

```bash
source ~/.bashrc
```

## 2. Telegram Plugin 설치

```
/plugin install telegram@claude-plugins-official
```

## 3. 설정 파일 적용

아빠가 준 md 파일의 내용을 Claude에 붙여넣고 (아니면 각 프로젝트 폴더에 이 md 파일을 저장하고) 이것대로 해달라고 한다.

## 4. Telegram Bot 토큰 등록

Telegram BotFather에서 bot을 만든 후에 그 토큰을 아래와 같이 붙여넣는다. 혹시 에러나면 한칸 띄고 `/tele…`를 입력.

```
/telegram:configure <YOUR_BOT_TOKEN>
```

> 토큰은 Telegram BotFather에서 새로 만든 것을 넣으면 됨

## 5. Claude Code 재시작

위 작업까지 다 끝나면 `exit`하고 Claude Code를 끝낸 뒤 다시 아래와 같이 입력하고 실행한다:

```bash
claude --dangerously-skip-permissions --channels plugin:telegram@claude-plugins-official
```

## 6. Bot 페어링

새로 생긴 봇에다가 아무 말이나 넣으면 아래와 같은 메시지가 나타나는데, 이걸 Claude Code에 넣는다:

```
/telegram:access pair adf682
```

## 7. 접근 정책 설정

마지막으로 아래와 같이 입력한다:

```
/telegram:access policy allowList
```

## 8. 확인 사항

- Claude에 `/plugin`이라고 치고 탭을 **installed**로 옮기면 Telegram plugin이 잘 설치되었는지 나옴. MCP까지 잘 설치되어 있어야 함.
- `~/.claude/channels/` → 이 전역 경로에는 아무것도 없어야 함. `telegram`이라고 있을 수도 있는데, 혹시 어떤 프로젝트의 Telegram plugin instance가 이용하고 있을지 모르니, 찾아서 로컬로 옮기고 전역 경로에서는 없애는 게 안전.
- 프로젝트 루트폴더의 `.claude/settings.json`에 아래와 같이 되어 있으면 됨:

  ```json
  {
    "env": {
      "TELEGRAM_STATE_DIR": "/home/<username>/<project>/.telegram"
    },
    "enabledPlugins": {
      "telegram@claude-plugins-official": true
    }
  }
  ```

- 프로젝트 루트폴더의 `.telegram/` 디렉토리에 `access.json` 및 기타 파일이 있으면 됨.
