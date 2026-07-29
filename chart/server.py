"""차트 렌더링 HTTP 서비스.

  POST /chart   {"source","symbol","from","to"}  → image/png
  GET  /health                                    → {"ok", "sources": [...]}
  GET  /sources                                   → 등록된 소스 상세

pinet 위에 있으므로 pi·claude 컨테이너가 컨테이너 이름으로 호출한다.
    curl -XPOST http://makesql_chart:8800/chart -d '{"source":"kor_daily",...}'

DB 는 조회 전용 계정(chart_ro)으로만 접근한다. 계정 정보는 환경변수로 받는다.
"""

import json
import os
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
            png = render.render_png(df, title.format(symbol=req["symbol"]))
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
