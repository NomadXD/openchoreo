# Copyright 2026 The OpenChoreo Authors
# SPDX-License-Identifier: Apache-2.0

"""The original trips service: ride history, written in Python.

GET /trips returns a rider's last trips. Every response says who answered in
the X-Served-By header, so the demo can count the split.
"""

import json
import os
import random
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("PORT", "8080"))
NAME = os.environ.get("APP_NAME", "trips-python")
# The Python service is slower; the rewrite is about latency.
LATENCY_MS = int(os.environ.get("LATENCY_MS", "120"))

TRIPS = [
    {"id": "t-1001", "from": "Colombo Fort", "to": "Bambalapitiya", "fare": 640},
    {"id": "t-1002", "from": "Nugegoda", "to": "Rajagiriya", "fare": 910},
    {"id": "t-1003", "from": "Dehiwala", "to": "Colombo Fort", "fare": 780},
]


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.startswith("/healthz"):
            self._send(200, {"ok": True})
            return
        time.sleep(LATENCY_MS / 1000 * random.uniform(0.8, 1.2))
        self._send(200, {"servedBy": NAME, "engine": "python", "trips": TRIPS})

    def _send(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("X-Served-By", NAME)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("", PORT), Handler).serve_forever()
