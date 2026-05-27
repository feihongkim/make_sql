#!/usr/bin/env python3
"""
Docker Claude Telegram 응답 모니터링 데몬 (폴링 방식)
모든 Docker Claude 봇의 Telegram 메시지를 /data3/teleAnswer/YYMMDD/container.md 에 저장

Usage: python tg_monitor.py          # 1회 수집
       python tg_monitor.py --loop   # 60초 간격 반복
"""
import os
import sys
import json
import asyncio
import signal
import time
from datetime import datetime
from pathlib import Path
from telethon import TelegramClient

API_ID = 39108291
API_HASH = "11d8c63d7e6a7930b8cd5535091695a5"
SESSION_FILE = "/home/feihong/.telegram_monitor_session"
BASE_DIR = Path("/data3/teleAnswer")
STATE_FILE = Path("/data3/teleAnswer/.last_ids.json")
PID_FILE = "/tmp/tg_monitor.pid"
POLL_INTERVAL = 60  # seconds

# bot_username -> container_name
BOT_MAP = {
    "Dart751623423942834_bot": "dart_claude",
    "dbSender7516123_bot": "dbsender_claude",
    "KIS7516_bot": "kis2_claude",
    "LS2930498290480_bot": "ls_claude",
    "sqlquery5716_bot": "makesql_claude",
    "MC751612356_bot": "mkyoutube_claude",
    "pyForMe7516_bot": "claude_python_forme",
    "ReadJson29387293847_bot": "readjson_claude",
    "RESTG7516Bot": "restg_claude",
    "RESTGo7516_bot": "restgo_claude",
    "JARVIS23847928379824_bot": "rstudio_claude",
    "Saver2938947298374_bot": "saver_claude",
    "StockTopReason751634_bot": "stocktopreason_claude",
    "Upbit7516234029384_bot": "upbit_claude",
}


def load_last_ids() -> dict:
    if STATE_FILE.exists():
        return json.loads(STATE_FILE.read_text())
    return {}


def save_last_ids(ids: dict):
    STATE_FILE.write_text(json.dumps(ids))


def get_file_path(container_name: str) -> Path:
    date_dir = BASE_DIR / datetime.now().strftime("%y%m%d")
    date_dir.mkdir(parents=True, exist_ok=True)
    return date_dir / f"{container_name}.md"


def append_message(container_name: str, direction: str, text: str, ts: datetime):
    if not text:
        return
    fp = get_file_path(container_name)
    time_str = ts.strftime("%H:%M:%S")
    marker = ">>> USER" if direction == "user" else "<<< BOT"
    with open(fp, "a", encoding="utf-8") as f:
        f.write(f"\n### [{time_str}] {marker}\n\n{text}\n\n---\n")


async def poll_once(client):
    """1회 폴링: 모든 봇 대화에서 새 메시지 수집"""
    last_ids = load_last_ids()
    new_count = 0

    for username, container in BOT_MAP.items():
        try:
            entity = await client.get_entity(f"@{username}")
            last_id = last_ids.get(container, 0)

            # 새 메시지만 가져오기 (min_id 이후)
            if last_id > 0:
                msgs = await client.get_messages(entity, min_id=last_id, limit=100)
            else:
                # 최초 실행: 최근 1개만 (기준점 설정)
                msgs = await client.get_messages(entity, limit=1)
                if msgs:
                    last_ids[container] = msgs[0].id
                continue

            if not msgs:
                continue

            # 오래된 것부터 처리
            for msg in reversed(msgs):
                if msg.id <= last_id:
                    continue
                direction = "user" if msg.out else "bot"
                append_message(container, direction, msg.text, msg.date)
                new_count += 1

            # 최신 ID 업데이트
            max_id = max(m.id for m in msgs)
            if max_id > last_id:
                last_ids[container] = max_id

        except Exception as e:
            print(f"  [WARN] {container}: {e}")

    save_last_ids(last_ids)
    return new_count


async def main(loop_mode: bool):
    client = TelegramClient(SESSION_FILE, API_ID, API_HASH)
    await client.connect()

    if not await client.is_user_authorized():
        print("[tg_monitor] 인증 필요. tg_send.py --auth 먼저 실행하세요.")
        return

    me = await client.get_me()
    print(f"[tg_monitor] 로그인: {me.first_name} (id={me.id})")
    print(f"[tg_monitor] 대상: {len(BOT_MAP)}개 봇")
    print(f"[tg_monitor] 저장: {BASE_DIR}/YYMMDD/container.md")

    # PID 파일
    with open(PID_FILE, "w") as f:
        f.write(str(os.getpid()))

    stop = False

    def _shutdown(signum, frame):
        nonlocal stop
        print(f"\n[tg_monitor] 종료 시그널 ({signum})")
        stop = True

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)

    try:
        if loop_mode:
            print(f"[tg_monitor] 루프 모드 ({POLL_INTERVAL}초 간격)")
            while not stop:
                count = await poll_once(client)
                if count > 0:
                    print(f"[{datetime.now().strftime('%H:%M:%S')}] {count}건 저장")
                await asyncio.sleep(POLL_INTERVAL)
        else:
            count = await poll_once(client)
            print(f"[tg_monitor] {count}건 저장 완료")
    finally:
        await client.disconnect()
        if os.path.exists(PID_FILE):
            os.remove(PID_FILE)
        print("[tg_monitor] 종료")


if __name__ == "__main__":
    loop_mode = "--loop" in sys.argv
    asyncio.run(main(loop_mode))
