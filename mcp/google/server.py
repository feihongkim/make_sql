"""Google MCP Server - Gmail + Calendar tools for Claude Code."""

import os
import json
import base64
from pathlib import Path
from datetime import datetime, timedelta

from google.auth.transport.requests import Request
from google.oauth2.credentials import Credentials
from google_auth_oauthlib.flow import InstalledAppFlow
from googleapiclient.discovery import build

from mcp.server.fastmcp import FastMCP

# OAuth scopes
SCOPES = [
    "https://www.googleapis.com/auth/gmail.modify",
    "https://www.googleapis.com/auth/calendar",
]

DIR = Path(__file__).parent
CRED_FILE = DIR / "credentials.json"
TOKEN_FILE = DIR / "token.json"

mcp = FastMCP("google", log_level="WARNING")


def get_credentials() -> Credentials:
    """Get or refresh OAuth credentials."""
    creds = None
    if TOKEN_FILE.exists():
        creds = Credentials.from_authorized_user_file(str(TOKEN_FILE), SCOPES)
    if not creds or not creds.valid:
        if creds and creds.expired and creds.refresh_token:
            creds.refresh(Request())
        else:
            flow = InstalledAppFlow.from_client_secrets_file(str(CRED_FILE), SCOPES)
            creds = flow.run_local_server(port=0)
        TOKEN_FILE.write_text(creds.to_json())
    return creds


def gmail_service():
    return build("gmail", "v1", credentials=get_credentials())


def calendar_service():
    return build("calendar", "v3", credentials=get_credentials())


# ── Gmail Tools ──


@mcp.tool()
def gmail_search(query: str, max_results: int = 20) -> str:
    """Gmail 검색. query는 Gmail 검색 문법 사용 (from:, subject:, is:unread 등)."""
    svc = gmail_service()
    resp = svc.users().messages().list(userId="me", q=query, maxResults=max_results).execute()
    messages = resp.get("messages", [])
    if not messages:
        return "검색 결과 없음"

    results = []
    for msg in messages:
        detail = svc.users().messages().get(userId="me", id=msg["id"], format="metadata",
                                             metadataHeaders=["From", "Subject", "Date"]).execute()
        headers = {h["name"]: h["value"] for h in detail.get("payload", {}).get("headers", [])}
        results.append({
            "id": msg["id"],
            "from": headers.get("From", ""),
            "subject": headers.get("Subject", ""),
            "date": headers.get("Date", ""),
            "snippet": detail.get("snippet", "")[:100],
        })
    return json.dumps(results, ensure_ascii=False, indent=2)


@mcp.tool()
def gmail_read(message_id: str) -> str:
    """Gmail 메시지 본문 읽기."""
    svc = gmail_service()
    msg = svc.users().messages().get(userId="me", id=message_id, format="full").execute()
    headers = {h["name"]: h["value"] for h in msg.get("payload", {}).get("headers", [])}

    body = ""
    payload = msg.get("payload", {})
    if "parts" in payload:
        for part in payload["parts"]:
            if part.get("mimeType") == "text/plain":
                data = part.get("body", {}).get("data", "")
                body = base64.urlsafe_b64decode(data).decode("utf-8", errors="replace")
                break
    elif "body" in payload and payload["body"].get("data"):
        body = base64.urlsafe_b64decode(payload["body"]["data"]).decode("utf-8", errors="replace")

    return json.dumps({
        "from": headers.get("From", ""),
        "subject": headers.get("Subject", ""),
        "date": headers.get("Date", ""),
        "body": body[:3000],
    }, ensure_ascii=False, indent=2)


@mcp.tool()
def gmail_trash(message_id: str) -> str:
    """Gmail 메시지를 휴지통으로 이동."""
    svc = gmail_service()
    svc.users().messages().trash(userId="me", id=message_id).execute()
    return f"메시지 {message_id} 휴지통 이동 완료"


@mcp.tool()
def gmail_spam(message_id: str) -> str:
    """Gmail 메시지를 스팸으로 처리."""
    svc = gmail_service()
    svc.users().messages().modify(
        userId="me", id=message_id,
        body={"addLabelIds": ["SPAM"], "removeLabelIds": ["INBOX"]}
    ).execute()
    return f"메시지 {message_id} 스팸 처리 완료"


@mcp.tool()
def gmail_delete(message_id: str) -> str:
    """Gmail 메시지를 영구 삭제 (복구 불가)."""
    svc = gmail_service()
    svc.users().messages().delete(userId="me", id=message_id).execute()
    return f"메시지 {message_id} 영구 삭제 완료"


@mcp.tool()
def gmail_batch_trash(query: str) -> str:
    """검색 조건에 맞는 Gmail 메시지를 모두 휴지통으로 이동."""
    svc = gmail_service()
    deleted = 0
    page_token = None
    while True:
        resp = svc.users().messages().list(
            userId="me", q=query, maxResults=100, pageToken=page_token
        ).execute()
        messages = resp.get("messages", [])
        if not messages:
            break
        for msg in messages:
            svc.users().messages().trash(userId="me", id=msg["id"]).execute()
            deleted += 1
        page_token = resp.get("nextPageToken")
        if not page_token:
            break
    return f"'{query}' 검색 결과 {deleted}건 휴지통 이동 완료"


# ── Calendar Tools ──


@mcp.tool()
def cal_list_events(
    time_min: str = "",
    time_max: str = "",
    max_results: int = 20,
    calendar_id: str = "primary",
) -> str:
    """Google Calendar 일정 조회. time_min/time_max는 'YYYY-MM-DD' 또는 'YYYY-MM-DDTHH:MM:SS' 형식."""
    svc = calendar_service()

    kwargs = {"calendarId": calendar_id, "maxResults": max_results,
              "singleEvents": True, "orderBy": "startTime", "timeZone": "Asia/Seoul"}

    if time_min:
        if "T" not in time_min:
            time_min += "T00:00:00+09:00"
        elif "+" not in time_min and "Z" not in time_min:
            time_min += "+09:00"
        kwargs["timeMin"] = time_min
    if time_max:
        if "T" not in time_max:
            time_max += "T23:59:59+09:00"
        elif "+" not in time_max and "Z" not in time_max:
            time_max += "+09:00"
        kwargs["timeMax"] = time_max

    resp = svc.events().list(**kwargs).execute()
    events = resp.get("items", [])
    if not events:
        return "일정 없음"

    results = []
    for ev in events:
        start = ev["start"].get("dateTime", ev["start"].get("date", ""))
        end = ev["end"].get("dateTime", ev["end"].get("date", ""))
        results.append({
            "id": ev["id"],
            "summary": ev.get("summary", "(제목 없음)"),
            "start": start,
            "end": end,
            "location": ev.get("location", ""),
            "status": ev.get("status", ""),
        })
    return json.dumps(results, ensure_ascii=False, indent=2)


@mcp.tool()
def cal_create_event(
    summary: str,
    start: str,
    end: str = "",
    location: str = "",
    description: str = "",
    all_day: bool = False,
    calendar_id: str = "primary",
) -> str:
    """Google Calendar 일정 생성. start/end는 'YYYY-MM-DD' (종일) 또는 'YYYY-MM-DDTHH:MM:SS' 형식."""
    svc = calendar_service()

    if all_day or (len(start) == 10 and "T" not in start):
        event_body = {
            "summary": summary,
            "start": {"date": start},
            "end": {"date": end if end else start},
        }
    else:
        if "+" not in start and "Z" not in start:
            start += "+09:00"
        if not end:
            # 기본 1시간
            from datetime import datetime as dt
            s = dt.fromisoformat(start.replace("+09:00", ""))
            e = s + timedelta(hours=1)
            end = e.strftime("%Y-%m-%dT%H:%M:%S") + "+09:00"
        elif "+" not in end and "Z" not in end:
            end += "+09:00"
        event_body = {
            "summary": summary,
            "start": {"dateTime": start, "timeZone": "Asia/Seoul"},
            "end": {"dateTime": end, "timeZone": "Asia/Seoul"},
        }

    if location:
        event_body["location"] = location
    if description:
        event_body["description"] = description

    ev = svc.events().insert(calendarId=calendar_id, body=event_body).execute()
    return json.dumps({
        "id": ev["id"],
        "summary": ev.get("summary"),
        "start": ev["start"].get("dateTime", ev["start"].get("date")),
        "end": ev["end"].get("dateTime", ev["end"].get("date")),
        "link": ev.get("htmlLink"),
    }, ensure_ascii=False, indent=2)


@mcp.tool()
def cal_update_event(
    event_id: str,
    summary: str = "",
    start: str = "",
    end: str = "",
    location: str = "",
    description: str = "",
    calendar_id: str = "primary",
) -> str:
    """Google Calendar 일정 수정. 변경할 필드만 입력."""
    svc = calendar_service()
    ev = svc.events().get(calendarId=calendar_id, eventId=event_id).execute()

    if summary:
        ev["summary"] = summary
    if start:
        if len(start) == 10:
            ev["start"] = {"date": start}
        else:
            if "+" not in start and "Z" not in start:
                start += "+09:00"
            ev["start"] = {"dateTime": start, "timeZone": "Asia/Seoul"}
    if end:
        if len(end) == 10:
            ev["end"] = {"date": end}
        else:
            if "+" not in end and "Z" not in end:
                end += "+09:00"
            ev["end"] = {"dateTime": end, "timeZone": "Asia/Seoul"}
    if location:
        ev["location"] = location
    if description:
        ev["description"] = description

    updated = svc.events().update(calendarId=calendar_id, eventId=event_id, body=ev).execute()
    return json.dumps({
        "id": updated["id"],
        "summary": updated.get("summary"),
        "start": updated["start"].get("dateTime", updated["start"].get("date")),
        "end": updated["end"].get("dateTime", updated["end"].get("date")),
    }, ensure_ascii=False, indent=2)


@mcp.tool()
def cal_delete_event(event_id: str, calendar_id: str = "primary") -> str:
    """Google Calendar 일정 삭제."""
    svc = calendar_service()
    svc.events().delete(calendarId=calendar_id, eventId=event_id).execute()
    return f"일정 {event_id} 삭제 완료"


if __name__ == "__main__":
    mcp.run(transport="stdio")
