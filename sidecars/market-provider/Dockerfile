# Fireman market data sidecar (image name: fireman-market-provider).
# Built with `uv sync --frozen --no-dev` so the resulting environment is
# reproducible from uv.lock.

FROM python:3.12-slim-bookworm AS builder

ENV UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_PYTHON_DOWNLOADS=never

COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

WORKDIR /app

COPY pyproject.toml uv.lock README.md ./
COPY fireman_market_provider ./fireman_market_provider

RUN uv sync --frozen --no-dev --no-install-project
RUN uv sync --frozen --no-dev


FROM python:3.12-slim-bookworm AS runner

WORKDIR /app

RUN groupadd --system --gid 1001 fireman \
 && useradd --system --uid 1001 --gid fireman --no-create-home fireman

COPY --from=builder --chown=fireman:fireman /app /app

ENV PATH="/app/.venv/bin:$PATH"

USER fireman
EXPOSE 18081

CMD ["uvicorn", "fireman_market_provider.app:app", "--host", "0.0.0.0", "--port", "18081"]
