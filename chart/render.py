"""캔들차트 렌더링.

ReadJson/draw.py 에서 그리기 로직만 가져와 정리했다.
가져오지 않은 것: 매수/매도 표시, RabbitMQ 메시지 처리.

박스 오버레이(2026-07-30): server.py 가 RESTGo 의 boxcalc 바이너리로 계산한
Box 목록을 boxes 인자로 넘기면 수평선으로 얹는다. Box 로직 자체는 이 코드에
없다 — 단일 소스는 RESTGo 저장소의 Go box/·stg/ 패키지다.
"""

import io
import matplotlib

matplotlib.use("Agg")  # 헤드리스. import 전에 지정해야 한다

import matplotlib.font_manager as fm  # noqa: E402
import matplotlib.pyplot as plt  # noqa: E402
import mplfinance as mpf  # noqa: E402
import pandas as pd  # noqa: E402


KOREAN_FONT = None


def _use_korean_font() -> None:
    """한글이 □□ 로 깨지지 않게 나눔폰트를 지정한다.

    koreanize-matplotlib 은 쓰지 않는다 — Python 3.12 에서 distutils 가 빠져
    import 자체가 실패한다. 이미지에 설치한 폰트를 직접 지정하는 편이 확실하다.
    """
    available = {f.name for f in fm.fontManager.ttflist}
    for name in ("NanumGothic", "NanumBarunGothic", "NanumSquare"):
        if name in available:
            global KOREAN_FONT
            KOREAN_FONT = name
            matplotlib.rcParams["font.family"] = name
            matplotlib.rcParams["axes.unicode_minus"] = False  # 마이너스가 네모로 깨지는 것 방지
            return
    # 조용히 넘어가면 한글이 깨진 채로 그려진다. 눈에 띄게 남긴다.
    print("경고: 나눔폰트를 찾지 못했습니다. 한글이 깨집니다.", flush=True)


_use_korean_font()

MA_WINDOWS = (5, 20, 60, 120)
BOLLINGER_WINDOW = 20
BOLLINGER_DEPTH = 2


def add_indicators(df: pd.DataFrame) -> pd.DataFrame:
    """이동평균선과 볼린저 밴드를 붙인다."""
    for w in MA_WINDOWS:
        df[f"MA{w}"] = df["Close"].rolling(window=w).mean()

    std = df["Close"].rolling(window=BOLLINGER_WINDOW).std()
    ma = df["Close"].rolling(window=BOLLINGER_WINDOW).mean()
    df["Upper_band"] = ma + BOLLINGER_DEPTH * std
    df["Lower_band"] = ma - BOLLINGER_DEPTH * std
    return df


def _addplots(df: pd.DataFrame):
    """구간이 짧아 전부 NaN 인 지표는 건너뛴다.
    mplfinance 는 전부 NaN 인 addplot 을 만나면 예외를 던진다."""
    specs = [
        ("MA5", dict(color="blue", width=0.75, label="MA5")),
        ("MA20", dict(color="orange", width=0.75, label="MA20")),
        ("MA60", dict(color="purple", width=0.75, label="MA60")),
        ("MA120", dict(color="brown", width=0.75, label="MA120")),
        ("Upper_band", dict(color="green", linestyle="dotted")),
        ("Lower_band", dict(color="red", linestyle="dotted")),
    ]
    return [
        mpf.make_addplot(df[col], **opts)
        for col, opts in specs
        if col in df and df[col].notna().any()
    ]


# boxcalc 출력의 kind/boxtype 코드 (RESTGo box/types.go 정합)
KIND_BOX, KIND_MAIN, KIND_DEF = 0, 1, 2
BOXTYPE_SUPPORT, BOXTYPE_RESIST = 0, 1


def _draw_boxes(ax, boxes: list, df: pd.DataFrame) -> None:
    """boxcalc 가 계산한 Box 목록을 가격 축에 수평선으로 얹는다.

    x 좌표: mplfinance 캔들은 정수 인덱스(0..n-1)로 배치되고, boxcalc 의
    pos 는 입력 캔들 인덱스라 서로 그대로 대응한다 (server.py 가 df 순서
    그대로 boxcalc 에 넘긴다). 선은 박스 생성 위치부터 우측 끝까지 긋는다.
    """
    n = len(df)
    y_lo, y_hi = ax.get_ylim()
    from matplotlib.lines import Line2D

    seen_labels = set()
    for b in boxes:
        price = b["price"]
        if price < y_lo or price > y_hi:
            continue  # 표시 구간 밖 박스는 축을 망가뜨리지 않게 건너뛴다
        x0 = max(0, min(b["pos"], n - 1))

        if b["kind"] == KIND_DEF:
            color, lw, ls, label, z = "#e74c3c", 1.8, "-", "DefBox", 5
        elif b["kind"] == KIND_MAIN:
            color, lw, ls, label, z = "#f39c12", 1.4, "--", "MainBox", 4
        elif b["boxtype"] == BOXTYPE_SUPPORT:
            color, lw, ls, label, z = "#27ae60", 1.0, ":", "SupportBox", 3
        else:
            color, lw, ls, label, z = "#2980b9", 1.0, ":", "ResistBox", 3

        ax.hlines(y=price, xmin=x0, xmax=n - 1, colors=color,
                  linewidths=lw, linestyles=ls, zorder=z)
        marker = "v" if b["boxtype"] == BOXTYPE_RESIST else "^"
        ax.scatter(x0, price, color=color, s=28, marker=marker, zorder=z + 1)
        if b["kind"] == KIND_DEF:
            ax.annotate(f" {price:,.0f}", xy=(n - 1, price), color=color,
                        fontsize=7, va="center", zorder=6)
        seen_labels.add((label, color, lw, ls))

    if seen_labels:
        handles = [Line2D([0], [0], color=c, lw=w, ls=s, label=lab)
                   for lab, c, w, s in sorted(seen_labels)]
        ax.legend(handles=handles, loc="upper left", fontsize=7)


def render_png(df: pd.DataFrame, title: str, boxes: list | None = None) -> bytes:
    """OHLCV DataFrame(DatetimeIndex)을 받아 PNG 바이트를 돌려준다.

    boxes 는 boxcalc 출력의 boxes 배열 (없으면 오버레이 생략).
    """
    if df.empty:
        raise ValueError("데이터가 없습니다 (해당 종목·기간에 행이 0건)")

    df = add_indicators(df.copy())

    # 상승 빨강 / 하락 파랑 (국내 관행)
    colors = mpf.make_marketcolors(
        up="r", down="b",
        volume={"up": "r", "down": "b"},
        edge={"up": "r", "down": "b"},
        wick={"up": "r", "down": "b"},
        ohlc={"up": "r", "down": "b"},
    )
    # ⚠️ make_mpf_style 은 rcParams 를 자체 값으로 덮어쓴다. rc= 로 폰트를 같이
    # 넘기지 않으면 위에서 rcParams 에 지정한 한글 폰트가 무시되고 □□ 로 깨진다.
    rc = {"axes.unicode_minus": False}
    if KOREAN_FONT:
        rc["font.family"] = KOREAN_FONT
    style = mpf.make_mpf_style(marketcolors=colors, gridstyle=":", gridcolor="gray", rc=rc)

    fig, axes = mpf.plot(
        df,
        type="candle",
        style=style,
        addplot=_addplots(df),
        volume=True,
        ylabel="Price",
        ylabel_lower="Volume",
        figscale=1.2,
        returnfig=True,
    )
    fig.suptitle(title, fontsize=16)

    if boxes:
        _draw_boxes(axes[0], boxes, df)

    buf = io.BytesIO()
    fig.savefig(buf, format="png", dpi=150, bbox_inches="tight")
    plt.close(fig)  # 닫지 않으면 요청마다 figure 가 쌓여 메모리를 먹는다
    return buf.getvalue()
