import asyncio
import ast
from collections.abc import Mapping
from pathlib import Path
import unittest

import httpx


def load_cookie_binding():
    source = Path("/app/opensandbox_server/api/proxy.py").read_text()
    tree = ast.parse(source)
    helper = next(
        (
            node
            for node in tree.body
            if isinstance(node, ast.FunctionDef)
            and node.name == "_bind_endpoint_cookie_authority"
        ),
        None,
    )
    if helper is None:
        return lambda _request, _headers: None
    proxy = next(
        (
            node
            for node in tree.body
            if isinstance(node, ast.AsyncFunctionDef) and node.name == "_proxy_http_request"
        ),
        None,
    )
    if proxy is None or not any(
        isinstance(node, ast.Call)
        and isinstance(node.func, ast.Name)
        and node.func.id == "_bind_endpoint_cookie_authority"
        for node in ast.walk(proxy)
    ):
        raise AssertionError("HTTP proxy does not apply cookie authority binding")
    namespace = {"Mapping": Mapping, "httpx": httpx}
    exec(compile(ast.Module(body=[helper], type_ignores=[]), "proxy.py", "exec"), namespace)
    return namespace["_bind_endpoint_cookie_authority"]


class CookieIsolationRegression(unittest.TestCase):
    def test_shared_client_keeps_only_endpoint_cookie_authority(self) -> None:
        seen_cookies: list[str | None] = []
        bind_endpoint_cookie_authority = load_cookie_binding()

        async def exercise() -> None:
            async def backend(request: httpx.Request) -> httpx.Response:
                seen_cookies.append(request.headers.get("cookie"))
                headers = {"set-cookie": "secret=value"} if len(seen_cookies) == 1 else {}
                return httpx.Response(204, headers=headers, request=request)

            async with httpx.AsyncClient(transport=httpx.MockTransport(backend)) as client:
                first = client.build_request("GET", "http://shared.internal/a")
                await client.send(first)

                second_headers: dict[str, str] = {}
                second = client.build_request(
                    "GET", "http://shared.internal/b", headers=second_headers
                )
                self.assertEqual(second.headers.get("cookie"), "secret=value")
                bind_endpoint_cookie_authority(second, second_headers)
                await client.send(second)

                authority_headers = {"Cookie": "endpoint=authority"}
                third = client.build_request(
                    "GET", "http://shared.internal/c", headers=authority_headers
                )
                bind_endpoint_cookie_authority(third, authority_headers)
                await client.send(third)

        asyncio.run(exercise())
        self.assertEqual(seen_cookies, [None, None, "endpoint=authority"])


if __name__ == "__main__":
    unittest.main()
