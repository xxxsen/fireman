"""Loaders for gzip-compressed fixture payloads under tests/testdata.

Unit tests must never hit real upstreams; recorded (or hand-written but
schema-faithful) payloads live in ``tests/testdata/*.json.gz`` and are loaded
through these helpers.
"""

from __future__ import annotations

import gzip
import json
from pathlib import Path
from typing import Any

import pandas as pd

TESTDATA_DIR = Path(__file__).parent / "testdata"


def load_json_gz(name: str) -> dict[str, Any]:
    """Load a gzip-compressed JSON fixture by file name."""
    path = TESTDATA_DIR / name
    with gzip.open(path, "rt", encoding="utf-8") as fh:
        return json.load(fh)


def load_dataframe_gz(name: str) -> pd.DataFrame:
    """Load a gzip-compressed tabular fixture as a DataFrame.

    The fixture must be a JSON object with ``columns`` (list of column names)
    and ``rows`` (list of row arrays), mirroring what the adapters would build
    from the upstream response.
    """
    payload = load_json_gz(name)
    return pd.DataFrame(payload["rows"], columns=payload["columns"])
