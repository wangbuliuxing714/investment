# Trading desk regression tests

Run `npm test` for transcript reconciliation, tool-result pairing and trading-mode tests.

For browser tests, start the web app with `npm run dev`, then run
`npm run test:browser`. These tests use a headless Chromium browser and mock all
application APIs, including the agent WebSocket. They do not start or stop the
trading agent or send orders.

The browser runner needs an installed `@playwright/test` package and its Chromium
browser. Set `DESK_PLAYWRIGHT_MODULE` to an existing package's absolute path to
reuse a workspace installation. Set `DESK_TEST_URL` to test another local port
(default: `http://127.0.0.1:3210`).

For stable browser runs, build the app and serve it with Vite preview on a
separate port, then point `DESK_TEST_URL` at it. This avoids development-server
hot reloads during tests. Demo activation and restoration are tested with mocked
settings responses and fake credentials; no exchange orders are submitted.

The browser suite also covers centralized prompt editing, refresh persistence,
model-switch preservation, explicit default restoration, failed-save draft
retention, and viewing symbols outside the trading allowlist. Prompt API and
model configuration writes are mocked in browser tests.
