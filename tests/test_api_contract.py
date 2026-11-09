"""Compare the Go branch's OpenAPI contract with the Python reference routes."""

import json
from pathlib import Path

from app.main import app


def test_contract_covers_existing_operations_and_documents_write_bodies():
    spec_path = Path(__file__).resolve().parents[1] / "api" / "openapi.json"
    spec = json.loads(spec_path.read_text(encoding="utf-8"))
    assert spec["openapi"].startswith("3.1.")
    expected = {(path, method) for path, operations in app.openapi()["paths"].items() for method in operations}
    actual = {(path, method) for path, operations in spec["paths"].items() for method in operations}
    assert actual == expected
    assert spec["paths"]["/health"]["get"]["security"] == []
    for path in ("/api/favorites", "/api/rating", "/api/archive/{gid}/{token}/start"):
        body = spec["paths"][path]["post"]["requestBody"]["content"]["application/json"]
        assert body["schema"]["$ref"]
        assert body["examples"]
