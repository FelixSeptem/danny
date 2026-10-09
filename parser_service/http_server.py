from __future__ import annotations

import argparse
import hashlib
import json
import logging
import threading
from concurrent.futures import ThreadPoolExecutor, TimeoutError as FutureTimeout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import unquote

from .core import CONTRACT_VERSION, ParseRequest, ParserError, Policy, fetch_url, parse_file, safe_failure

logger = logging.getLogger("danny.parser")


class JobStore:
    def __init__(self, root: Path, staging_root: Path):
        self.root = root
        self.staging_root = staging_root
        self.root.mkdir(parents=True, exist_ok=True)
        self.lock = threading.Lock()
        self.executor = ThreadPoolExecutor(max_workers=2, thread_name_prefix="danny-parser")
        self.parse_executor = ThreadPoolExecutor(max_workers=2, thread_name_prefix="danny-parser-convert")

    def path(self, job_id: str) -> Path:
        return self.root / f"{job_id}.json"

    def load(self, job_id: str) -> dict:
        path = self.path(job_id)
        if not path.is_file():
            raise ParserError("not_found", "parser job was not found")
        return json.loads(path.read_text(encoding="utf-8"))

    def save(self, payload: dict) -> None:
        path = self.path(payload["job_id"])
        temporary = path.with_suffix(".tmp")
        temporary.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
        temporary.replace(path)

    def submit(self, request: ParseRequest) -> dict:
        request.validate()
        with self.lock:
            for path in self.root.glob("*.json"):
                existing = json.loads(path.read_text(encoding="utf-8"))
                if existing.get("idempotency_key") == request.idempotency_key:
                    if existing.get("content_hash") != request.content_hash:
                        raise ParserError("idempotency_conflict", "idempotency key belongs to another source")
                    return existing
            payload = {
                "job_id": request.job_id,
                "contract_version": CONTRACT_VERSION,
                "idempotency_key": request.idempotency_key,
                "content_hash": request.content_hash,
                "staging_object_id": request.staging_object_id,
                "source_fingerprint": request.source_fingerprint,
                "filename": request.filename,
                "format_hint": request.format_hint,
                "policy": request.policy.__dict__,
                "status": "accepted",
                "request": request.__dict__ | {"policy": request.policy.__dict__},
            }
            self.save(payload)
            return payload

    def recover(self) -> None:
        for temporary in self.root.glob("*.tmp"):
            temporary.unlink(missing_ok=True)
        for path in self.root.glob("*.json"):
            try:
                payload = json.loads(path.read_text(encoding="utf-8"))
                if payload.get("status") not in {"accepted", "running"}:
                    continue
                request = ParseRequest.from_dict(payload["request"])
                logger.info("recovering parser job %s", request.job_id)
                self.executor.submit(self.run, request)
            except Exception:
                payload = {"job_id": path.stem, "contract_version": CONTRACT_VERSION, "status": "failed", "failure": {"code": "recovery_failed", "message": "parser job recovery failed", "retryable": False}}
                self.save(payload)

    def run(self, request: ParseRequest) -> None:
        payload = self.load(request.job_id)
        if payload.get("status") == "cancelled":
            return
        payload["status"] = "running"
        self.save(payload)
        logger.info("running parser job %s attempt", request.job_id)
        try:
            future = self.parse_executor.submit(parse_file, request, self.staging_root)
            try:
                result = future.result(timeout=max(request.policy.timeout_seconds, 1))
            except FutureTimeout as exc:
                future.cancel()
                raise ParserError("timeout", "parser conversion timed out", retryable=True) from exc
            if self.load(request.job_id).get("status") == "cancelled":
                return
            payload.update({"status": "succeeded", "result": result, "failure": None, "provider_mode": result.get("provider_mode", "local"), "parser_version": result.get("parser_version", "")})
        except Exception as exc:
            failure = safe_failure(exc)
            payload.update({"status": failure["code"], "result": None, "failure": failure, "cleanup_state": "complete"})
            logger.warning("parser job %s failed: %s", request.job_id, failure["code"])
        self.save(payload)


class Handler(BaseHTTPRequestHandler):
    server_version = "danny-parser/0.1"

    def send_json(self, status: int, payload: dict) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def read_json(self) -> dict:
        length = int(self.headers.get("Content-Length", "0"))
        if length > 1024 * 1024:
            raise ParserError("resource_limited", "request body is too large")
        return json.loads(self.rfile.read(length))

    @property
    def store(self) -> JobStore:
        return self.server.job_store  # type: ignore[attr-defined]

    def do_GET(self) -> None:
        path = unquote(self.path).split("?", 1)[0]
        try:
            if path == "/v1/health/live":
                return self.send_json(200, {"status": "ok", "contract_version": CONTRACT_VERSION})
            if path == "/v1/health/ready":
                return self.send_json(200, {"status": "ready", "contract_version": CONTRACT_VERSION, "local": True, "pageindex_cloud": False})
            if path == "/v1/capabilities":
                return self.send_json(200, {"contract_version": CONTRACT_VERSION, "formats": sorted(__import__("parser_service.core", fromlist=["SUPPORTED_FORMATS"]).SUPPORTED_FORMATS), "parser_version": "danny-parser-0.1.0", "providers": ["local"]})
            parts = path.split("/")
            if len(parts) == 5 and parts[:3] == ["", "v1", "parser"] and parts[3] == "jobs":
                return self.send_json(200, self.store.load(parts[4]))
            if len(parts) == 6 and parts[:3] == ["", "v1", "parser"] and parts[3] == "jobs" and parts[5] == "result":
                payload = self.store.load(parts[4])
                if payload.get("status") != "succeeded":
                    return self.send_json(409, payload)
                return self.send_json(200, payload["result"])
            return self.send_json(404, {"code": "not_found", "message": "endpoint not found"})
        except Exception as exc:
            failure = safe_failure(exc)
            return self.send_json(404 if failure["code"] == "not_found" else 400, failure)

    def do_POST(self) -> None:
        path = unquote(self.path).split("?", 1)[0]
        try:
            if path == "/v1/parser/urls":
                body = self.read_json()
                url = str(body.get("url", ""))
                policy = Policy(**{k: body.get("policy", {})[k] for k in Policy.__dataclass_fields__ if k in body.get("policy", {})})
                content, filename, format_hint = fetch_url(url, policy)
                content_hash = hashlib.sha256(content).hexdigest()
                object_id = "url-" + content_hash[:32]
                object_path = self.store.staging_root / object_id
                self.store.staging_root.mkdir(parents=True, exist_ok=True)
                object_path.write_bytes(content)
                request = ParseRequest(CONTRACT_VERSION, body.get("job_id", object_id), body.get("idempotency_key", "url:" + content_hash), object_id, body.get("source_fingerprint", "url:" + hashlib.sha256(url.encode()).hexdigest()[:24]), content_hash, filename, format_hint, policy)
                payload = self.store.submit(request)
                if payload["status"] == "accepted":
                    self.store.executor.submit(self.store.run, request)
                return self.send_json(202, payload)
            if path == "/v1/parser/jobs":
                request = ParseRequest.from_dict(self.read_json())
                payload = self.store.submit(request)
                if payload["status"] == "accepted":
                    self.store.executor.submit(self.store.run, request)
                return self.send_json(202, payload)
            parts = path.split("/")
            if len(parts) == 6 and parts[:3] == ["", "v1", "parser"] and parts[3] == "jobs" and parts[5] == "cancel":
                payload = self.store.load(parts[4])
                if payload["status"] in {"accepted", "running"}:
                    payload["status"] = "cancelled"
                    self.store.save(payload)
                return self.send_json(200, payload)
            return self.send_json(404, {"code": "not_found", "message": "endpoint not found"})
        except Exception as exc:
            failure = safe_failure(exc)
            return self.send_json(409 if failure["code"] == "idempotency_conflict" else 400, failure)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8090)
    parser.add_argument("--staging-root", required=True)
    parser.add_argument("--jobs-root", required=True)
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    server.job_store = JobStore(Path(args.jobs_root), Path(args.staging_root))  # type: ignore[attr-defined]
    server.job_store.recover()  # type: ignore[attr-defined]
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
