"""차트 렌더링 HTTP 서비스.

  POST /chart   {"source","symbol","from","to"[,"boxes":true]}  → image/png
  GET  /health                                    → {"ok", "sources": [...]}
  GET  /sources                                   → 등록된 소스 상세

"boxes": true 면 RESTGo 의 boxcalc 바이너리(/app/bin/boxcalc, 정적 Go)로
Box/MainBox/DefBox 를 계산해 수평선으로 얹는다. Box 로직의 단일 소스는
RESTGo 저장소이며, 바이너리 갱신은 RESTGo 의 deploy/build_boxcalc.sh →
이미지 재빌드로 한다.

pinet 위에 있으므로 pi·claude 컨테이너가 컨테이너 이름으로 호출한다.
    curl -XPOST http://makesql_chart:8800/chart -d '{"source":"kor_daily",...}'

DB 는 조회 전용 계정(chart_ro)으로만 접근한다. 계정 정보는 환경변수로 받는다.
"""

import json
import os
import subprocess
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pandas as pd
import pymssql
import yaml

import render

PORT = int(os.environ.get("CHART_PORT", "8800"))
CONFIG_PATH = os.environ.get("CHART_SOURCES", "/app/config/chart_sources.yaml")
DB_USER = os.environ.get("MSSQL_CHART_RO_USER", "chart_ro")
DB_PASSWORD = os.environ.get("MSSQL_CHART_RO_PASSWORD", "")
QUERY_TIMEOUT = int(os.environ.get("CHART_QUERY_TIMEOUT", "60"))
MAX_ROWS = int(os.environ.get("CHART_MAX_ROWS", "5000"))
BOXCALC_BIN = os.environ.get("BOXCALC_BIN", "/app/bin/boxcalc")


def compute_boxes(df, symbol: str) -> list:
    """boxcalc(RESTGo Go 바이너리)로 Box 목록을 계산한다.

    입력은 fetch_ohlcv 가 돌려준 DataFrame 그대로 — 행 순서가 boxcalc 의
    pos 인덱스가 되므로 정렬을 바꾸면 안 된다. 실패는 예외로 올린다
    (boxes 를 명시적으로 요청한 호출자에게 조용한 누락은 오판을 만든다).
    """
    payload = {
        "shcode": symbol,
        "candles": [
            {
                "date": idx.strftime("%Y%m%d"),
                "open": float(row.Open), "high": float(row.High),
                "low": float(row.Low), "close": float(row.Close),
                "volume": float(row.Volume),
            }
            for idx, row in zip(df.index, df.itertuples())
        ],
    }
    proc = subprocess.run(
        [BOXCALC_BIN], input=json.dumps(payload).encode(),
        capture_output=True, timeout=30,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"boxcalc 실패: {proc.stderr.decode(errors='replace').strip()}")
    return json.loads(proc.stdout)["boxes"]


def load_config() -> dict:
    """요청마다 읽는다. 소스를 추가할 때 컨테이너를 재시작하지 않아도 된다."""
    with open(CONFIG_PATH, encoding="utf-8") as f:
        return yaml.safe_load(f)


def fetch_ohlcv(cfg: dict, source: str, symbol: str, date_from: str, date_to: str) -> pd.DataFrame:
    src = cfg["sources"].get(source)
    if src is None:
        raise KeyError(f"등록되지 않은 소스: {source} (가능: {', '.join(cfg['sources'])})")
    server = cfg["servers"][src["server"]]

    # 컬럼명은 설정에서 오고 값은 파라미터로 넘긴다.
    # 컬럼명에 대괄호를 씌워 예약어(OPEN, CLOSE, DATE)를 피한다.
    def col(name: str) -> str:
        return "[" + src[name].strip("[]") + "]"

    where = [f"{col('symbol')} = %s", f"{col('date')} BETWEEN %s AND %s"]
    if src.get("where"):
        where.append(f"({src['where']})")

    sql = (
        f"SELECT TOP {MAX_ROWS} {col('date')} AS d, {col('open')} AS o, {col('high')} AS h, "
        f"{col('low')} AS l, {col('close')} AS c, {col('volume')} AS v "
        f"FROM {src['table']} WHERE {' AND '.join(where)} ORDER BY {col('date')}"
    )

    conn = pymssql.connect(
        server=server["host"], port=int(server.get("port", 1433)),
        user=DB_USER, password=DB_PASSWORD, database=src["db"],
        timeout=QUERY_TIMEOUT, login_timeout=15,
    )
    try:
        df = pd.read_sql(sql, conn, params=(symbol, date_from, date_to))
    finally:
        conn.close()

    if df.empty:
        return df

    df["d"] = pd.to_datetime(df["d"].astype(str), format="%Y%m%d", errors="coerce")
    df = df.dropna(subset=["d"]).set_index("d")
    df.index.name = "Date"
    df = df.rename(columns={"o": "Open", "h": "High", "l": "Low", "c": "Close", "v": "Volume"})
    return df.astype(float)


def fetch_name(cfg: dict, source: str, symbol: str) -> str:
    """종목·지수 코드에 대응하는 표시 이름을 찾는다.

    소스에 name: {table, key, value} 가 있을 때만 조회한다.
    실패해도 차트는 그려야 하므로 예외를 삼키고 빈 문자열을 돌려준다.
    """
    src = cfg["sources"][source]
    look = src.get("name")
    if not look:
        return ""
    server = cfg["servers"][src["server"]]
    sql = (f"SELECT TOP 1 [{look['value']}] FROM {look['table']} "
           f"WHERE [{look['key']}] = %s")
    try:
        conn = pymssql.connect(
            server=server["host"], port=int(server.get("port", 1433)),
            user=DB_USER, password=DB_PASSWORD,
            database=look.get("db", src["db"]),
            timeout=QUERY_TIMEOUT, login_timeout=15,
        )
        try:
            cur = conn.cursor()
            cur.execute(sql, (symbol,))
            row = cur.fetchone()
        finally:
            conn.close()
    except Exception as e:
        print(f"이름 조회 실패({source}/{symbol}): {e}", flush=True)
        return ""
    if not row or not row[0]:
        return ""
    return _tidy_name(str(row[0]))


def _tidy_name(s: str) -> str:
    """업종명은 자간을 공백으로 채워 저장돼 있다 ("대   형  주", "운 수 창 고").

    토막이 전부 한 글자면 자간 공백으로 보고 붙인다. 그렇지 않으면
    정상 낱말 사이 공백이므로 건드리지 않는다 ("KRX 기후변화 솔루션").
    """
    parts = s.split()
    if len(parts) > 1 and all(len(p) == 1 for p in parts):
        return "".join(parts)
    return " ".join(parts)


class Handler(BaseHTTPRequestHandler):
    def _send(self, status: int, body: bytes, content_type: str):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _json(self, status: int, obj: dict):
        self._send(status, json.dumps(obj, ensure_ascii=False).encode(), "application/json; charset=utf-8")

    def log_message(self, fmt, *args):  # 기본 stderr 접근 로그를 간결하게
        print(f"{self.address_string()} {fmt % args}", flush=True)

    def do_GET(self):
        try:
            cfg = load_config()
        except Exception as e:
            return self._json(500, {"error": f"설정 로드 실패: {e}"})

        if self.path == "/health":
            return self._json(200, {"ok": True, "sources": list(cfg["sources"])})
        if self.path == "/sources":
            return self._json(200, cfg["sources"])
        return self._json(404, {"error": "not found — GET /health, /sources 또는 POST /chart"})

    def do_POST(self):
        if self.path != "/chart":
            return self._json(404, {"error": "not found — POST /chart"})

        try:
            length = int(self.headers.get("Content-Length", "0"))
            req = json.loads(self.rfile.read(length) or b"{}")
        except Exception as e:
            return self._json(400, {"error": f"JSON 파싱 실패: {e}"})

        missing = [k for k in ("source", "symbol", "from", "to") if not req.get(k)]
        if missing:
            return self._json(400, {"error": f"필수 항목 누락: {', '.join(missing)}"})

        try:
            cfg = load_config()
            df = fetch_ohlcv(cfg, req["source"], req["symbol"], req["from"], req["to"])
            title = req.get("title") or cfg["sources"][req["source"]].get("title", "{symbol}")
            name = fetch_name(cfg, req["source"], req["symbol"])
            boxes = compute_boxes(df, req["symbol"]) if req.get("boxes") else None
            png = render.render_png(df, title.format(symbol=req["symbol"], name=name).strip(), boxes=boxes)
        except KeyError as e:
            return self._json(400, {"error": str(e).strip("'")})
        except ValueError as e:
            return self._json(404, {"error": str(e)})
        except Exception as e:
            return self._json(500, {"error": f"{type(e).__name__}: {e}"})

        return self._send(200, png, "image/png")


if __name__ == "__main__":
    if not DB_PASSWORD:
        raise SystemExit("MSSQL_CHART_RO_PASSWORD 가 비어 있습니다. run_chart.sh 로 기동하세요.")
    print(f"chart: http://0.0.0.0:{PORT}", flush=True)
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
