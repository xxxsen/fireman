"""Pytest configuration for market-provider tests."""

import os

# Inline AKShare calls so unittest.mock patches apply (subprocess fork breaks mocks).
os.environ.setdefault("MARKET_PROVIDER_DISABLE_SUBPROCESS", "1")
