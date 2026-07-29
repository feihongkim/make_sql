"""캔들차트 렌더링.

ReadJson/draw.py 에서 그리기 로직만 가져와 정리했다.
가져오지 않은 것: 매수/매도 표시, 박스 표시, RabbitMQ 메시지 처리.
박스는 종목코드 매핑과 데이터 갱신 문제를 푼 뒤 얹는다.
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


def render_png(df: pd.DataFrame, title: str) -> bytes:
    """OHLCV DataFrame(DatetimeIndex)을 받아 PNG 바이트를 돌려준다."""
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

    fig, _ = mpf.plot(
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

    buf = io.BytesIO()
    fig.savefig(buf, format="png", dpi=150, bbox_inches="tight")
    plt.close(fig)  # 닫지 않으면 요청마다 figure 가 쌓여 메모리를 먹는다
    return buf.getvalue()
