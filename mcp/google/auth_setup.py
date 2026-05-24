"""Headless OAuth 인증 - 브라우저 없는 서버용.

1. URL 출력 → 폰/PC 브라우저에서 열기
2. Google 로그인 → 권한 허용
3. 리다이렉트되면 브라우저 주소창의 전체 URL 복사
4. 터미널에 붙여넣기
"""

import os
from pathlib import Path
from urllib.parse import urlparse, parse_qs
from google_auth_oauthlib.flow import Flow

os.environ["OAUTHLIB_RELAX_TOKEN_SCOPE"] = "1"

SCOPES = [
    "https://www.googleapis.com/auth/gmail.modify",
    "https://www.googleapis.com/auth/calendar",
]

DIR = Path(__file__).parent
CRED_FILE = DIR / "credentials.json"
TOKEN_FILE = DIR / "token.json"

REDIRECT_URI = "http://localhost:8080"

flow = Flow.from_client_secrets_file(
    str(CRED_FILE),
    scopes=SCOPES,
    redirect_uri=REDIRECT_URI,
)

auth_url, _ = flow.authorization_url(
    access_type="offline",
    include_granted_scopes="true",
    prompt="consent",
)

print("=" * 60)
print("아래 URL을 브라우저(PC/폰)에서 열어주세요:")
print("=" * 60)
print()
print(auth_url)
print()
print("=" * 60)
print("Google 로그인 후 '이 사이트에 연결할 수 없습니다' 에러가 나옵니다.")
print("→ 그때 브라우저 주소창의 전체 URL을 복사하세요.")
print("  (http://localhost:8080/?code=... 형태)")
print("=" * 60)
print()

redirect_url = input("리다이렉트된 전체 URL을 붙여넣으세요: ").strip()

# URL에서 code 추출
parsed = urlparse(redirect_url)
params = parse_qs(parsed.query)
if "code" not in params:
    print("❌ URL에서 인증 코드를 찾을 수 없습니다.")
    exit(1)

code = params["code"][0]
flow.fetch_token(code=code)
creds = flow.credentials
TOKEN_FILE.write_text(creds.to_json())
print(f"\n✅ token.json 저장 완료: {TOKEN_FILE}")
