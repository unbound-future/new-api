#!/usr/bin/env python3
import argparse
import json
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description="Validate COSLOG JSONL without printing payload contents")
    parser.add_argument("files", nargs="+", type=Path)
    parser.add_argument("--marker", default="")
    parser.add_argument("--min-request-bytes", type=int, default=0)
    args = parser.parse_args()

    summary = {
        "files": len(args.files),
        "bytes": 0,
        "scanned_records": 0,
        "records": 0,
        "skipped_records": 0,
        "invalid_json_lines": 0,
        "marker_records": 0,
        "status_codes": {},
        "stream_records": 0,
        "non_stream_records": 0,
        "empty_request_body": 0,
        "empty_response_body": 0,
        "min_request_body_bytes": None,
        "min_response_body_bytes": None,
    }

    for path in args.files:
        summary["bytes"] += path.stat().st_size
        with path.open("rb") as handle:
            for line in handle:
                if not line.strip():
                    continue
                summary["scanned_records"] += 1
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    summary["invalid_json_lines"] += 1
                    continue

                request_body = row.get("request_body") or ""
                response_body = row.get("response_body") or ""
                request_size = len(request_body.encode()) if isinstance(request_body, str) else len(json.dumps(request_body).encode())
                response_size = len(response_body.encode()) if isinstance(response_body, str) else len(json.dumps(response_body).encode())
                if request_size < args.min_request_bytes:
                    summary["skipped_records"] += 1
                    continue
                summary["records"] += 1
                if request_size == 0:
                    summary["empty_request_body"] += 1
                if response_size == 0:
                    summary["empty_response_body"] += 1
                current_request_min = summary["min_request_body_bytes"]
                current_response_min = summary["min_response_body_bytes"]
                summary["min_request_body_bytes"] = request_size if current_request_min is None else min(current_request_min, request_size)
                summary["min_response_body_bytes"] = response_size if current_response_min is None else min(current_response_min, response_size)

                if args.marker and args.marker in request_body:
                    summary["marker_records"] += 1
                if row.get("is_stream"):
                    summary["stream_records"] += 1
                else:
                    summary["non_stream_records"] += 1
                status = str(row.get("status_code"))
                summary["status_codes"][status] = summary["status_codes"].get(status, 0) + 1

    print(json.dumps(summary, ensure_ascii=False, indent=2, sort_keys=True))
    if summary["invalid_json_lines"] or summary["empty_request_body"] or summary["empty_response_body"]:
        raise SystemExit(2)
    if args.marker and summary["marker_records"] == 0:
        raise SystemExit(3)


if __name__ == "__main__":
    main()
