# Pi Telegram 연동 설정 가이드

이 문서는 **Pi Coding Agent**에 Telegram 연동을 설정하는 방법을 설명합니다.
현재 RESTGo pi 컨테이너에 설정된 `~/.pi/agent/extensions/telegram/index.ts`가 기준입니다.

---

## 개요

Pi Extension을 통해 Telegram과 연동하면:

- **실시간 메시지 수신**: Telegram으로 보낸 메시지가 자동으로 pi의 user prompt로 전달됨
- **에이전트 응답 송신**: pi가 `telegram_send` 도구로 Telegram 채팅에 응답 전송
- **사진 지원**: `telegram_send_photo`로 차트·이미지 전송
- **멀티 컨테이너**: BotFather에서 여러 봇을 생성하면 각 pi 컨테이너가 독립 연동 가능

제공되는 도구:
| 도구 | 기능 |
|------|------|
| `telegram_send` | 채팅에 텍스트 메시지 전송 |
| `telegram_send_photo` | 채팅에 사진/이미지 전송 (로컬 파일 or URL) |
| `telegram_updates` | 최근 수신 메시지 조회 |
| `telegram_get_me` | 봇 자기 정보 확인 |
| `telegram_get_chat` | 채팅 메타데이터 조회 |

---

## 1. 사전 준비

### 1.1 BotFather로 봇 생성

Telegram에서 [@BotFather](https://t.me/BotFather)와 대화:

```
/newbot
```

봇 이름과 username을 설정하면 **Bot Token**이 발급됩니다:

```
1234567890:ABCdefGHIjklMNOpqrsTUVwxyz-12345678
```

> **멀티 컨테이너 팁**: 컨테이너마다 별도 봇을 만들거나, 여러 컨테이너가 같은 봇을 공유할 수 있습니다.
> 같은 봇을 공유하면 모든 컨테이너가 동일한 채팅을 수신하고 경쟁 응답합니다.
> **컨테이너별 독립 연동을 원하면 각각 다른 봇을 생성하세요.**

### 1.2 봇 토큰 저장 위치 결정

토큰은 두 가지 방식 중 하나로 제공합니다:

| 방식 | 경로 | 우선순위 |
|------|------|----------|
| 환경변수 | `TELEGRAM_BOT_TOKEN` | 높음 (우선) |
| 파일 | `~/.pi/agent/telegram_token` | 낮음 (폴백) |

---

## 2. Extension 설치

### 2.1 Extension 파일 복사

현재 RESTGo 컨테이너의 `/home/node/.pi/agent/extensions/telegram/index.ts` 를
대상 컨테이너의 동일 경로에 복사합니다.

```bash
# 대상 컨테이너에서
mkdir -p ~/.pi/agent/extensions/telegram

# index.ts 파일을 복사 (scp, git clone, 또는 수동 복사)
# 아래 내용을 그대로 ~/.pi/agent/extensions/telegram/index.ts 로 저장
```

### 2.2 index.ts 전체 코드

<details>
<summary>전체 코드 (클릭하여 펼치기)</summary>

```typescript
/**
 * Telegram Extension for pi
 *
 * Setup:
 *   1. Create a bot via @BotFather on Telegram
 *   2. export TELEGRAM_BOT_TOKEN="your_token"
 *   3. Run pi (auto-loads from ~/.pi/agent/extensions/telegram/)
 *
 * Tools provided:
 *   - telegram_send       : Send a message to a chat
 *   - telegram_send_photo : Send a photo to a chat
 *   - telegram_updates    : Poll for recent updates received by the bot
 *   - telegram_get_me     : Get bot identity info
 *   - telegram_get_chat   : Get chat metadata
 */

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { Bot, InputFile, type Context } from "grammy";
import { readFileSync, createReadStream, existsSync, writeFileSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join, extname } from "node:path";
import { createHash } from "node:crypto";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function getBotToken(): string {
  const token = process.env.TELEGRAM_BOT_TOKEN;
  if (token) return token;

  const tokenFile = join(homedir(), ".pi", "agent", "telegram_token");
  try {
    const fileToken = readFileSync(tokenFile, "utf-8").trim();
    if (fileToken) return fileToken;
  } catch {
    // file doesn't exist or can't be read
  }

  throw new Error(
    "TELEGRAM_BOT_TOKEN is not set.  Create a bot with @BotFather then:\n" +
      "  export TELEGRAM_BOT_TOKEN=123456:ABC-DEF1234gh...\n" +
      "Or save the token to ~/.pi/agent/telegram_token"
  );
}

function formatUpdate(update: any): string {
  const msg = update.message ?? update.edited_message ?? update.channel_post;
  if (!msg) {
    if (update.callback_query) {
      const cq = update.callback_query;
      return `[callback_query] from=${cq.from?.first_name} data=${cq.data} msg_id=${cq.message?.message_id}`;
    }
    return `[update] update_id=${update.update_id} type=${Object.keys(update).filter(k => k !== "update_id").join(",")}`;
  }

  const from = msg.from
    ? `${msg.from.first_name ?? ""}${msg.from.last_name ? " " + msg.from.last_name : ""} (@${msg.from.username ?? "no-username"}, id=${msg.from.id})`
    : "unknown";

  const parts: string[] = [`[${msg.chat.type}] ${from}`];
  if (msg.text) parts.push(`text="${msg.text.slice(0, 200)}"`);
  if (msg.caption) parts.push(`caption="${msg.caption.slice(0, 200)}"`);
  if (msg.photo) parts.push(`photo (${msg.photo.length} sizes)`);
  if (msg.document) parts.push(`document="${msg.document.file_name}"`);
  if (msg.voice) parts.push(`voice (${msg.voice.duration}s)`);
  if (msg.sticker) parts.push(`sticker="${msg.sticker.emoji}"`);
  if (msg.location) parts.push(`location=(${msg.location.latitude},${msg.location.longitude})`);
  parts.push(`msg_id=${msg.message_id} chat_id=${msg.chat.id}`);

  return parts.join(" | ");
}

// ---------------------------------------------------------------------------
// Extension
// ---------------------------------------------------------------------------

export default function (pi: ExtensionAPI) {
  let _bot: Bot | null = null;
  let _stopPolling: (() => Promise<void>) | null = null;
  let _lastUpdateId = 0;

  const TYPING_INTERVAL_MS = 4000;
  const _typingTimers = new Map<string, ReturnType<typeof setInterval>>();

  function bot(): Bot {
    if (!_bot) {
      const token = getBotToken();
      _bot = new Bot(token);
    }
    return _bot;
  }

  function startTyping(chatId: string | number) {
    const id = String(chatId);
    if (_typingTimers.has(id)) return;
    const b = bot();
    b.api.sendChatAction(id, "typing").catch(() => {});
    const timer = setInterval(() => {
      b.api.sendChatAction(id, "typing").catch(() => {});
    }, TYPING_INTERVAL_MS);
    _typingTimers.set(id, timer);
  }

  function stopTyping(chatId: string | number) {
    const id = String(chatId);
    const timer = _typingTimers.get(id);
    if (timer) {
      clearInterval(timer);
      _typingTimers.delete(id);
    }
  }

  function stopAllTyping() {
    for (const [id, timer] of _typingTimers) {
      clearInterval(timer);
    }
    _typingTimers.clear();
  }

  function formatIncomingMessage(msg: any): string {
    const from = msg.from?.first_name ?? "unknown";
    const chatId = msg.chat.id;
    let text = msg.text ?? msg.caption ?? "";
    const chatType = msg.chat.type;
    let prefix = `[Telegram ${chatType} from ${from} (chat_id=${chatId})]`;
    if (msg.photo && (msg as any).__localPhotoPath) {
      prefix += ` (photo saved to ${(msg as any).__localPhotoPath})`;
      if (!text) text = "[Photo message]";
    }
    if (msg.document && (msg as any).__localDocPath) {
      prefix += ` (document saved to ${(msg as any).__localDocPath})`;
      if (!text) text = `[Document: ${msg.document.file_name}]`;
    }
    if (!text) text = "[non-text message]";
    return `${prefix}: ${text}\n\nReply using telegram_send with chat_id=${chatId}.`;
  }

  async function downloadAndSave(fileId: string, fileName: string): Promise<string> {
    const b = bot();
    const file = await b.api.getFile(fileId);
    const ext = extname(fileName) || ".jpg";
    const hash = createHash("md5").update(fileId).digest("hex").slice(0, 8);
    const dir = join(homedir(), ".pi", "agent", "downloads");
    mkdirSync(dir, { recursive: true });
    const localPath = join(dir, `telegram_${hash}${ext}`);
    const url = `https://api.telegram.org/file/bot${getBotToken()}/${file.file_path}`;
    const response = await fetch(url);
    if (!response.ok) throw new Error(`Failed to download file: ${response.status}`);
    const buffer = Buffer.from(await response.arrayBuffer());
    writeFileSync(localPath, buffer);
    return localPath;
  }

  function startPolling() {
    const b = bot();
    let stopped = false;

    async function poll() {
      while (!stopped) {
        try {
          const updates = await b.api.getUpdates({
            offset: _lastUpdateId + 1,
            timeout: 30,
            allowed_updates: ["message", "edited_message"],
          });
          for (const update of updates) {
            _lastUpdateId = Math.max(_lastUpdateId, update.update_id);
            const msg = update.message ?? update.edited_message;
            if (!msg) continue;
            if (msg.photo && msg.photo.length > 0) {
              try {
                const largestPhoto = msg.photo[msg.photo.length - 1];
                const localPath = await downloadAndSave(largestPhoto.file_id, `photo_${msg.message_id}.jpg`);
                (msg as any).__localPhotoPath = localPath;
              } catch (err: any) {
                (msg as any).__localPhotoPath = `(download failed: ${err.message})`;
              }
            }
            if (msg.document) {
              try {
                const localPath = await downloadAndSave(msg.document.file_id, msg.document.file_name ?? `doc_${msg.message_id}`);
                (msg as any).__localDocPath = localPath;
              } catch (err: any) {
                (msg as any).__localDocPath = `(download failed: ${err.message})`;
              }
            }
            if (msg.text || msg.caption || msg.photo || msg.document) {
              startTyping(msg.chat.id);
              pi.sendUserMessage(formatIncomingMessage(msg));
            }
          }
        } catch (err: any) {
          if (!stopped) {
            await new Promise(r => setTimeout(r, 5000));
          }
        }
      }
    }

    poll();
    _stopPolling = async () => {
      stopped = true;
    };
  }

  // ---- tools ---------------------------------------------------------------

  pi.registerTool({
    name: "telegram_send",
    label: "Telegram Send",
    description:
      "Send a text message to a Telegram chat. The chatId can be a numeric user ID, group ID, or @channel username.",
    promptSnippet: "Send a Telegram message to a chat",
    parameters: Type.Object({
      chatId: Type.String({ description: "Target chat ID (e.g. 123456789) or @username" }),
      text: Type.String({ description: "Message text (Markdown supported)" }),
      parseMode: Type.Optional(Type.String({ description: 'Parse mode: "HTML" or "MarkdownV2" (default plain text)' })),
    }),
    async execute(_toolCallId, params) {
      stopTyping(params.chatId);
      const { message_id, chat } = await bot().api.sendMessage(params.chatId, params.text, {
        parse_mode: (params.parseMode as any) ?? undefined,
      });
      return {
        content: [{ type: "text", text: `✅ Sent to ${chat.type} chat "${chat.first_name ?? chat.title ?? chat.id}" (msg_id=${message_id})` }],
        details: { messageId: message_id, chatId: chat.id },
      };
    },
  });

  pi.registerTool({
    name: "telegram_send_photo",
    label: "Telegram Send Photo",
    description: "Send a photo to a Telegram chat. The photo can be a local file path or a URL. Max 10MB for photos.",
    promptSnippet: "Send a photo/image to a Telegram chat",
    parameters: Type.Object({
      chatId: Type.String({ description: "Target chat ID (e.g. 123456789) or @username" }),
      photo: Type.String({ description: "Local file path (e.g. /tmp/chart.png) or public URL to the photo" }),
      caption: Type.Optional(Type.String({ description: "Optional caption for the photo" })),
    }),
    async execute(_toolCallId, params) {
      stopTyping(params.chatId);
      let inputFile: any;
      if (params.photo.startsWith("http://") || params.photo.startsWith("https://")) {
        inputFile = params.photo;
      } else {
        if (!existsSync(params.photo)) {
          return {
            content: [{ type: "text", text: `❌ File not found: ${params.photo}` }],
            details: { error: "file_not_found" },
          };
        }
        inputFile = new InputFile(params.photo);
      }
      const opts: any = {};
      if (params.caption) opts.caption = params.caption;
      const { message_id, chat, photo: photoInfo } = await bot().api.sendPhoto(params.chatId, inputFile, opts);
      const fileId = photoInfo?.[photoInfo.length - 1]?.file_id ?? "unknown";
      return {
        content: [{ type: "text", text: `✅ Photo sent to ${chat.type} chat "${chat.first_name ?? chat.title ?? chat.id}" (msg_id=${message_id}, file_id=${fileId})` }],
        details: { messageId: message_id, chatId: chat.id, fileId },
      };
    },
  });

  pi.registerTool({
    name: "telegram_updates",
    label: "Telegram Updates",
    description: "Poll for recent incoming messages / updates received by the bot.  Returns updates from the last 24 hours (up to the requested limit).  Use this to see what people have sent to the bot.",
    promptSnippet: "Read recent Telegram messages received by the bot",
    parameters: Type.Object({
      limit: Type.Optional(Type.Number({ description: "Max updates to return (default 20, max 100)" })),
    }),
    async execute(_toolCallId, params) {
      const limit = Math.min(params.limit ?? 20, 100);
      const updates = await bot().api.getUpdates({ limit, timeout: 5, allowed_updates: ["message", "edited_message", "channel_post"] });
      if (updates.length === 0) {
        return { content: [{ type: "text", text: "No recent updates." }], details: { count: 0 } };
      }
      const lines = updates.map((u, i) => `${i + 1}. ${formatUpdate(u)}`);
      return {
        content: [{ type: "text", text: `${updates.length} update(s):\n\n${lines.join("\n")}` }],
        details: { count: updates.length, updates: updates.map(u => u.update_id) },
      };
    },
  });

  pi.registerTool({
    name: "telegram_get_me",
    label: "Telegram Get Me",
    description: "Get the bot's own identity (name, username, ID).",
    parameters: Type.Object({}),
    async execute() {
      const me = await bot().api.getMe();
      return {
        content: [{ type: "text", text: `Bot: @${me.username} (id=${me.id}, name="${me.first_name}")` }],
        details: { id: me.id, username: me.username, firstName: me.first_name },
      };
    },
  });

  pi.registerTool({
    name: "telegram_get_chat",
    label: "Telegram Get Chat",
    description: "Get metadata about a Telegram chat (user, group, channel) by ID or @username.",
    parameters: Type.Object({
      chatId: Type.String({ description: "Chat ID or @username" }),
    }),
    async execute(_toolCallId, params) {
      const chat = await bot().api.getChat(params.chatId);
      const info: string[] = [`ID: ${chat.id}`, `Type: ${chat.type}`];
      if ("title" in chat && chat.title) info.push(`Title: ${chat.title}`);
      if ("first_name" in chat && chat.first_name) info.push(`Name: ${chat.first_name}`);
      if ("username" in chat && chat.username) info.push(`Username: @${chat.username}`);
      if ("description" in chat && chat.description) info.push(`Description: ${chat.description}`);
      return {
        content: [{ type: "text", text: info.join("\n") }],
        details: { id: chat.id, type: chat.type },
      };
    },
  });

  // ---- lifecycle -----------------------------------------------------------

  pi.on("session_start", async (_event, ctx) => {
    try {
      const me = await bot().api.getMe();
      ctx.ui.notify(`Telegram: @${me.username} connected`, "info");
      if (!_stopPolling) {
        startPolling();
        ctx.ui.notify("Telegram: real-time polling started", "info");
      }
    } catch (err: any) {
      ctx.ui.notify(`Telegram: ${err.message}`, "error");
    }
  });

  pi.on("session_shutdown", async () => {
    stopAllTyping();
    if (_stopPolling) {
      await _stopPolling();
      _stopPolling = null;
    }
  });
}
```

</details>

---

## 3. 의존성 설치

Extension이 `grammy` 라이브러리에 의존하므로, extension 디렉토리에 `package.json`을 생성하고 npm install 합니다:

```bash
cd ~/.pi/agent/extensions/telegram

# package.json 생성
cat > package.json << 'EOF'
{
  "name": "pi-telegram-extension",
  "private": true,
  "dependencies": {
    "grammy": "^1.34.0"
  }
}
EOF

npm install
```

---

## 4. 토큰 설정

둘 중 하나의 방법을 선택:

### 방법 A: 환경변수 (권장 — Docker 컨테이너 친화적)

```bash
export TELEGRAM_BOT_TOKEN="1234567890:ABCdefGHIjklMNOpqrsTUVwxyz-12345678"
```

Docker 컨테이너에서는 `docker run -e` 또는 `docker-compose.yml`의 `environment`로 주입:

```yaml
# docker-compose.yml
services:
  pi-agent:
    environment:
      - TELEGRAM_BOT_TOKEN=1234567890:ABCdefGHIjklMNOpqrsTUVwxyz-12345678
```

### 방법 B: 파일 저장

```bash
echo "1234567890:ABCdefGHIjklMNOpqrsTUVwxyz-12345678" > ~/.pi/agent/telegram_token
chmod 600 ~/.pi/agent/telegram_token
```

---

## 5. 실행 및 확인

```bash
# pi 실행
pi

# TUI 하단 알림에서 확인:
#   Telegram: @YourBot connected
#   Telegram: real-time polling started
```

### 5.1 동작 확인

Telegram에서 봇에게 메시지를 보내면 pi가 자동으로 수신하여 agent prompt로 전달합니다.
pi는 `telegram_send` 도구를 사용해 응답을 Telegram으로 전송합니다.

### 5.2 수동 확인

pi 세션 내에서 다음 명령을 실행:

```
telegram_get_me → 봇 정보 출력
telegram_updates → 최근 수신 메시지 확인
```

---

## 6. 접근 제어 (선택)

Extension은 기본적으로 봇에게 메시지를 보내는 **모든 사용자**의 메시지를 처리합니다.
특정 사용자만 허용하려면 `session_start` 핸들러에 필터를 추가:

```typescript
pi.on("session_start", async (_event, ctx) => {
  // ... 기존 코드 ...
});

// 메시지 수신 시 발신자 검증
// 기존 startPolling() 내 msg 처리부에 추가:
const ALLOWED_USERS = [7723743534];  // 허용할 user ID 목록
if (msg.text || msg.caption || msg.photo || msg.document) {
  // 발신자 필터
  const userId = msg.from?.id;
  if (userId && ALLOWED_USERS.includes(userId)) {
    startTyping(msg.chat.id);
    pi.sendUserMessage(formatIncomingMessage(msg));
  }
}
```

또는 프로젝트별 `.telegram/access.json` 같은 자체 설정 파일을 사용할 수도 있습니다
(RESTGo의 `.telegram/access.json`은 프로젝트 전용 Python 봇 설정 — pi extension과는 별개입니다).

---

## 7. 멀티 컨테이너 구성

여러 pi 컨테이너에서 Telegram 연동이 필요한 경우:

### 옵션 A: 개별 봇 (권장)

각 컨테이너마다 BotFather에서 새 봇을 생성 → 독립 토큰 발급.

```
컨테이너 A → Bot Token A → @MyBot_A
컨테이너 B → Bot Token B → @MyBot_B
```

**장점**: 완전 독립적, 컨테이너 간 간섭 없음
**단점**: 봇 개수만큼 BotFather에서 생성 필요

### 옵션 B: 공유 봇 + 접근 제어

하나의 봇을 여러 컨테이너가 공유하되, 각 컨테이너가 담당할 chat_id나 사용자만 필터링.

```
컨테이너 A → 동일한 Bot Token → user_id=7723743534 메시지만 처리
컨테이너 B → 동일한 Bot Token → user_id=1234567890 메시지만 처리
```

**장점**: 봇 1개만 관리
**단점**: 모든 컨테이너가 동일한 업데이트를 수신 → 필터로 분기해야 함,
        한 컨테이너가 `telegram_updates` 호출 시 업데이트 소비로 인한 경합 가능성

### 옵션 C: 공유 봇 + 컨테이너 라우팅 에이전트

하나의 봇으로 모든 메시지를 수신하는 "라우터 컨테이너"를 두고,
메시지 내용에 따라 적절한 컨테이너로 전달.

```
사용자 → @MyBot → 컨테이너 R (라우터)
                      ├─ "프로젝트 A 분석해줘" → 컨테이너 A
                      └─ "프로젝트 B 빌드해줘" → 컨테이너 B
```

**장점**: 봇 1개, 중앙 관리
**단점**: 라우터 구현 필요, 복잡도 증가

---

## 8. 문제 해결

### 봇이 응답하지 않음

```bash
# 1. 토큰 확인
echo $TELEGRAM_BOT_TOKEN
cat ~/.pi/agent/telegram_token

# 2. 봇 연결 테스트
curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getMe"

# 3. pi 실행 시 TUI 알림 확인
# "Telegram: @YourBot connected" → 정상
# "Telegram: 401 Unauthorized" → 토큰 오류
```

### 메시지가 중복 처리됨

- 여러 컨테이너가 같은 봇 토큰을 사용 중인지 확인
- `_lastUpdateId` 추적이 컨테이너별로 독립적인지 확인

### 사진 다운로드 실패

- 컨테이너의 `/home/node/.pi/agent/downloads` 디렉토리 쓰기 권한 확인
- 네트워크에서 `api.telegram.org` 접근 가능 여부 확인

### Extension이 로드되지 않음

```bash
# 파일 존재 확인
ls -la ~/.pi/agent/extensions/telegram/index.ts

# npm 의존성 설치 확인
ls ~/.pi/agent/extensions/telegram/node_modules/grammy

# pi를 --extension 플래그로 명시적 로드
pi -e ~/.pi/agent/extensions/telegram/index.ts
```

---

## 9. RESTGo 특이사항

현재 RESTGo 컨테이너의 설정:

- **Extension 경로**: `/home/node/.pi/agent/extensions/telegram/index.ts`
- **토큰 방식**: `~/.pi/agent/telegram_token` 파일 사용
- **팁**: `~/.pi/agent/telegram_token` 파일만 다른 컨테이너로 복사하고
  `~/.pi/agent/extensions/telegram/` 디렉토리를 통째로 복사하면 동일 설정 재현 가능

---

## 참고

- [Pi Extensions 문서](https://github.com/earendil-works/pi-coding-agent/blob/main/docs/extensions.md)
- [Telegram Bot API](https://core.telegram.org/bots/api)
- [grammy 라이브러리](https://grammy.dev/)
