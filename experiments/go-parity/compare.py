"""Compare the real Python ASGI routes with the isolated Go replay handlers."""

from __future__ import annotations

import asyncio
import base64
import json
import os
import subprocess
from pathlib import Path

from fastapi import FastAPI
from httpx import ASGITransport, AsyncClient

from app.config import Settings
from app.eh.exceptions import ExceedLimitError
from app.eh.parser import parse_detail_page, parse_image_page, parse_list_page
from app.opds2.router import router as opds2_router
from app.stream.router import router as stream_router

BASE = Path(__file__).parent
FIXTURES = BASE / "fixtures"
LIST_HTML = (FIXTURES / "latest.html").read_text()
DETAIL = parse_detail_page((FIXTURES / "detail.html").read_text(), "e-hentai.org")
IMAGE_BYTES = (FIXTURES / "image.jpg").read_bytes()


class FixtureService:
    """Small EHService adapter; parser inputs and outputs are shared fixtures."""

    archive = None

    def __init__(self, quota: bool = False) -> None:
        self.quota = quota

    async def search_galleries(self, **_kwargs):
        return parse_list_page(LIST_HTML)

    async def get_detail_doc(self, *_args):
        return DETAIL

    async def get_mytags(self):
        return {}

    async def get_image(self, *_args):
        image_page = FIXTURES / ("image_509.html" if self.quota else "image_page.html")
        parsed = parse_image_page(image_page.read_text())
        if parsed.is_509:
            raise ExceedLimitError("image quota exceeded")
        return IMAGE_BYTES, "image/jpeg"


async def python_capture(quota: bool = False):
    app = FastAPI()
    app.include_router(opds2_router)
    app.include_router(stream_router)
    app.state.settings = Settings()
    app.state.service = FixtureService(quota)

    async with AsyncClient(
        transport=ASGITransport(app=app), base_url="http://python-test"
    ) as client:
        listing_response = await client.get("/opds/v2.0/gallery")
        listing = listing_response.json()
        publication = listing["publications"][0]
        self_href = next(link["href"] for link in publication["links"] if link["rel"] == "self")
        detail_response = await client.get(self_href)
        detail = detail_response.json()
        first_image = await client.get(detail["readingOrder"][0]["href"])
        return listing, publication, detail, detail_response.headers["content-type"], first_image


def go_capture(quota: bool = False):
    env = os.environ.copy()
    if quota:
        env["FIXTURE_509"] = "1"
    else:
        env.pop("FIXTURE_509", None)
    output = subprocess.check_output(["go", "run", ".", "replay"], cwd=BASE, env=env)
    return json.loads(output)


def main() -> None:
    go = go_capture()
    py_listing, py_publication, py_detail, py_publication_type, py_image = asyncio.run(python_capture())

    assert go["publication"]["metadata"]["identifier"] == py_publication["metadata"]["identifier"]
    assert go["publication"]["metadata"]["numberOfPages"] == py_publication["metadata"]["numberOfPages"] == 12
    go_cursor = go["listing"]["links"][0]["href"].split("next=", 1)[1]
    py_next = next(link["href"] for link in py_listing["links"] if link["rel"] == "next")
    py_cursor = py_next.split("next=", 1)[1]
    assert go_cursor == py_cursor == "2753175-1786365950", (go_cursor, py_cursor)
    assert go["detail"]["metadata"]["numberOfPages"] == py_detail["metadata"]["numberOfPages"] == 12
    assert go["detail"]["readingOrder"][0]["href"] == py_detail["readingOrder"][0]["href"]
    assert go["imageStatus"] == py_image.status_code == 200
    assert go["imageType"] == py_image.headers["content-type"] == "image/jpeg"
    assert base64.b64decode(go["imageBase64"]) == py_image.content == IMAGE_BYTES

    go_quota = go_capture(quota=True)
    _, _, _, _, py_quota = asyncio.run(python_capture(quota=True))
    assert go_quota["imageStatus"] == py_quota.status_code == 429
    assert go_quota["imageRetryAfter"] == py_quota.headers["retry-after"] == "300"

    print(
        "PASS: identity, page count, opaque cursor, readingOrder URL, valid JPEG "
        "status/type/bytes, and 509 -> 429/Retry-After match real Python ASGI routes"
    )


if __name__ == "__main__":
    main()
