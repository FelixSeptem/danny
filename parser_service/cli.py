from __future__ import annotations

import argparse
import json
from pathlib import Path

from .core import ParseRequest, ParserError, parse_file, safe_failure, status_payload

EXIT_CODES = {
    "unsupported": 10,
    "resource_limited": 11,
    "timeout": 12,
    "cancelled": 13,
    "provider_failed": 20,
    "failed": 1,
}


def main() -> int:
    parser = argparse.ArgumentParser(description="Danny OpenKB-inspired document parser")
    parser.add_argument("--request", required=True)
    parser.add_argument("--result", required=True)
    parser.add_argument("--staging-root", required=True)
    args = parser.parse_args()
    request_path = Path(args.request)
    result_path = Path(args.result)
    try:
        request = ParseRequest.from_dict(json.loads(request_path.read_text(encoding="utf-8")))
        result = parse_file(request, Path(args.staging_root))
        payload = status_payload(request.job_id, "succeeded", result=result)
    except Exception as exc:
        failure = safe_failure(exc)
        payload = status_payload(request.job_id if "request" in locals() else "", failure["code"], failure=failure)
    result_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    status = payload["status"]
    return 0 if status == "succeeded" else EXIT_CODES.get(status, 1)


if __name__ == "__main__":
    raise SystemExit(main())
