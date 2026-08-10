#!/usr/bin/env python3
"""A self-contained stand-in for a Django development server.

It prints the same readiness line a real Django runserver prints, so the
runtime's django profile detects readiness, without requiring Django installed.
"""
import http.server
import socketserver
import sys

PORT = 8000
if len(sys.argv) >= 2 and sys.argv[1] == "runserver":
    if len(sys.argv) >= 3:
        PORT = int(sys.argv[2].split(":")[-1])

print(f"Starting development server at http://127.0.0.1:{PORT}/", flush=True)
print("Quit the server with CONTROL-C.", flush=True)


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok\n")

    def log_message(self, *args):
        pass


with socketserver.TCPServer(("", PORT), Handler) as httpd:
    httpd.serve_forever()
