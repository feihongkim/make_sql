#!/usr/bin/env python3
"""
Docker Claude 봇에 사용자 계정으로 메시지 전송 및 응답 수신
Usage:
  python tg_send.py <bot_username> <message>              # 전송만
  python tg_send.py --ask <bot_username> <message>        # 전송 후 응답 대기
  python tg_send.py --auth <phone> [2fa_password]         # 최초 인증
"""
import sys
import asyncio
import time
from telethon import TelegramClient
from telethon.tl.types import User

API_ID = 39108291
API_HASH = "11d8c63d7e6a7930b8cd5535091695a5"
SESSION_FILE = "/home/feihong/.telegram_user_session"

async def send_message(bot_username: str, message: str, retries: int = 3):
    for attempt in range(retries):
        try:
            async with TelegramClient(SESSION_FILE, API_ID, API_HASH) as client:
                await client.send_message(bot_username, message)
                print(f"[tg_send] {bot_username} 에게 메시지 전송 완료")
                return
        except Exception as e:
            if "database is locked" in str(e) and attempt < retries - 1:
                import time
                time.sleep(1 + attempt)
                continue
            raise

async def ask_message(bot_username: str, message: str, timeout: int = 300):
    """메시지 전송 후 응답 대기 (최대 timeout초)"""
    async with TelegramClient(SESSION_FILE, API_ID, API_HASH) as client:
        # 현재 마지막 메시지 ID 기록
        msgs = await client.get_messages(bot_username, limit=1)
        last_id = msgs[0].id if msgs else 0

        # 메시지 전송
        await client.send_message(bot_username, message)
        print(f"[tg_send] {bot_username} 에게 메시지 전송 완료, 응답 대기 중 (최대 {timeout}초)...", flush=True)

        # 응답 폴링
        deadline = time.time() + timeout
        prev_response = ""
        last_activity = time.time()

        while time.time() < deadline:
            await asyncio.sleep(3)
            new_msgs = await client.get_messages(bot_username, min_id=last_id, limit=50)
            # 봇이 보낸 메시지만 필터
            bot_msgs = [m for m in reversed(new_msgs) if m.out is False and m.id > last_id]
            if bot_msgs:
                combined = "\n".join(m.text or "" for m in bot_msgs if m.text)
                if combined and combined != prev_response:
                    last_activity = time.time()
                    prev_response = combined
                    last_id = bot_msgs[-1].id
                # 마지막 활동 후 5초 대기 후 응답 완료로 간주
                elif combined and (time.time() - last_activity) > 5:
                    break

        if prev_response:
            print(f"\n[{bot_username} 응답]\n{prev_response}")
        else:
            print(f"[tg_send] 응답 없음 (timeout {timeout}초 초과)")

async def auth_only(phone: str, password: str = None):
    """최초 1회 인증용"""
    import getpass
    client = TelegramClient(SESSION_FILE, API_ID, API_HASH)
    pw = password or getpass.getpass("Telegram 2단계 인증 비밀번호: ")
    await client.start(phone=phone, password=pw)
    me = await client.get_me()
    print(f"[tg_send] 인증 완료: {me.first_name} (@{me.username})")
    await client.disconnect()

if __name__ == "__main__":
    if len(sys.argv) >= 2 and sys.argv[1] == "--auth":
        if len(sys.argv) < 3:
            print("Usage: tg_send.py --auth <phone_number> [2fa_password]")
            sys.exit(1)
        pw = sys.argv[3] if len(sys.argv) >= 4 else None
        asyncio.run(auth_only(sys.argv[2], pw))
    elif len(sys.argv) >= 2 and sys.argv[1] == "--ask":
        # 전송 + 응답 대기: python tg_send.py --ask <bot_username> <message>
        if len(sys.argv) < 4:
            print("Usage: tg_send.py --ask <bot_username> <message>")
            sys.exit(1)
        bot_username = sys.argv[2]
        message = " ".join(sys.argv[3:])
        asyncio.run(ask_message(bot_username, message))
    elif len(sys.argv) >= 3:
        bot_username = sys.argv[1]
        message = " ".join(sys.argv[2:])
        asyncio.run(send_message(bot_username, message))
    else:
        print("Usage:")
        print("  tg_send.py --auth <phone_number> [2fa]     # 최초 인증")
        print("  tg_send.py --ask <bot_username> <message>  # 전송 + 응답 수신")
        print("  tg_send.py <bot_username> <message>        # 전송만")
        sys.exit(1)
