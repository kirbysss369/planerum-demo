"""Check the deployed API. --full calls the configured model and builds files."""

import argparse
import json
import os
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


def request(base, token, path, payload=None, timeout=60):
    data = json.dumps(payload).encode() if payload is not None else None
    headers = {"Authorization": f"Bearer {token}"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    req = Request(base + path, data=data, headers=headers, method="POST" if data else "GET")
    try:
        with urlopen(req, timeout=timeout) as response:
            raw = response.read()
    except HTTPError as exc:
        raise RuntimeError(f"HTTP {exc.code}: {exc.read(500).decode(errors='replace')}") from exc
    except URLError as exc:
        raise RuntimeError(f"connection failed: {exc.reason}") from exc
    if path == "/healthz":
        return raw.decode().strip()
    if path.startswith("/artifacts/"):
        return len(raw)
    return json.loads(raw)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="http://127.0.0.1:8080")
    parser.add_argument("--full", action="store_true", help="generate and export a full bundle (may incur Cloud.ru cost)")
    parser.add_argument("--async", dest="asynchronous", action="store_true", help="use persistent generation job with --full")
    parser.add_argument("--title", default="Демонстрационная проверка API")
    args = parser.parse_args()
    if args.asynchronous and not args.full:
        parser.error("--async requires --full")
    token = os.getenv("SERVICE_TOKEN", "")
    base = args.base.rstrip("/")
    if request(base, token, "/healthz") != "ok":
        raise RuntimeError("health check failed")
    spec = {
        "title": args.title, "kind": "general", "audience": "Начинающие специалисты",
        "entry_level": "Начальный", "delivery": "Очно", "requested": 24,
        "attestation": "Защита практической работы",
    }
    result = request(base, token, "/v1/agent", {"action": "create", "spec": spec}, timeout=240)
    if result.get("questions"):
        raise RuntimeError(f"unexpected clarification: {result['questions']}")
    program = result["program"]
    print(f"outline: id={program['id']} revision={program['revision']} state={program['state']}")
    if program["state"] != "outline_ready" or any(i["severity"] == "error" for i in result.get("issues", [])):
        raise RuntimeError("outline is not ready for approval")
    if not args.full:
        print("outline check passed; add --full for approval, materials and exports")
        return
    program = request(base, token, "/v1/agent", {
        "action": "approve_outline", "program_id": program["id"], "revision": program["revision"]
    })["program"]
    if program["state"] != "approved":
        raise RuntimeError("approval failed")
    if args.asynchronous:
        queued = request(base, token, "/v1/agent", {
            "action": "generate_async", "program_id": program["id"], "revision": program["revision"]
        })
        job_id = queued["job"]["id"]
        deadline = time.monotonic() + 1800
        while True:
            status = request(base, token, "/v1/agent", {"action": "job_status", "job_id": job_id})
            if status["job"]["state"] == "failed":
                raise RuntimeError(f"generation job failed: {status['job'].get('error')}")
            if status["job"]["state"] == "done":
                program = status["program"]
                break
            if time.monotonic() > deadline:
                raise RuntimeError("generation job exceeded 30 minutes")
            time.sleep(2)
    else:
        program = request(base, token, "/v1/agent", {
            "action": "generate", "program_id": program["id"], "revision": program["revision"]
        }, timeout=1500)["program"]
    if program["state"] != "ready":
        raise RuntimeError(f"materials are not ready: {program.get('issues')}")
    exported = request(base, token, "/v1/agent", {
        "action": "export", "program_id": program["id"], "revision": program["revision"]
    }, timeout=1500)
    required = {"program.json", "program.docx", "lectures.pptx", "tests.xml", "validation-report.json"}
    if set(exported.get("artifacts", {})) != required:
        raise RuntimeError(f"unexpected artifact set: {exported.get('artifacts')}")
    for name, path in exported["artifacts"].items():
        size = request(base, token, path, timeout=120)
        if not size:
            raise RuntimeError(f"empty artifact: {name}")
        print(f"{name}: {size} bytes")
    print(f"full check passed: id={program['id']} revision={program['revision']}")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, KeyError, ValueError) as error:
        print(f"smoke check failed: {error}", file=sys.stderr)
        sys.exit(1)
