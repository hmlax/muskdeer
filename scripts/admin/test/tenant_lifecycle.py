"""HTTP tenant onboarding regression against the running infra-docker stack.

Business changes use public APIs. The only test-only infrastructure read obtains
our own newly generated CAPTCHA from Redis; authentication and RBAC stay enabled.
"""

from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request

try:
    import yaml
except ImportError as error:
    raise SystemExit("Install test dependencies: python3 -m pip install -r scripts/admin/test/requirements.txt") from error


ROOT = Path(__file__).resolve().parents[3]

def require(condition, message):
    if not condition:
        raise AssertionError(message)


def timestamp(days=0):
    return (datetime.now(timezone.utc) + timedelta(days=days)).isoformat().replace("+00:00", "Z")


def password():
    return "Qa7!" + secrets.token_urlsafe(15)


def private_json(path, value):
    # Create the file with restrictive permissions before writing credentials.
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")


class Report:
    def __init__(self, output_root=None, title="租户生命周期自动化测试"):
        self.title = title
        self.run_id = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S") + "_" + secrets.token_hex(3)
        self.output_root = Path(output_root) if output_root else ROOT / ".cache/admin-artifacts" / "tenant-e2e"
        self.directory = self.output_root / self.run_id
        self.directory.mkdir(parents=True, mode=0o700)
        self.steps = []
        self.secrets = []
        self.resources = {"tenants": []}
        self.active = None
        self.ready = False
        self.failure_trace = None

    def redact(self, value):
        text = str(value)
        for secret in sorted(self.secrets, key=len, reverse=True):
            if secret:
                text = text.replace(secret, "[REDACTED]")
        return text

    @contextmanager
    def step(self, name):
        result = {"name": name, "status": "RUNNING", "requests": []}
        self.steps.append(result)
        self.active = result
        start = time.monotonic()
        try:
            yield
            result["status"] = "PASS"
        except Exception as error:
            result["status"] = "FAIL"
            result["error"] = self.redact(error)
            raise
        finally:
            result["duration_ms"] = round((time.monotonic() - start) * 1000)
            self.active = None
            print(result["status"] + " " + name, flush=True)

    def save(self, base, credentials, failure=None):
        passed = sum(item["status"] == "PASS" for item in self.steps)
        result = {
            "run_id": self.run_id, "finished_at": timestamp(), "base_url": base,
            "status": "FAIL" if failure else "PASS", "passed": passed,
            "total": len(self.steps), "failure": self.redact(failure) if failure else None,
            "resources": self.resources, "steps": self.steps,
            "failure_trace": self.failure_trace,
            "http_requests": sum(len(item["requests"]) for item in self.steps),
            "expected_rejections": sum(request["status"] >= 400 and request["status"] in request["expected_statuses"]
                                       for item in self.steps for request in item["requests"]),
        }
        private_json(self.directory / "report.json", result)
        private_json(self.directory / "credentials.json", credentials)
        private_json(self.output_root / "latest.json", {
            "run_id": self.run_id, "status": result["status"], "ready": credentials.get("ready", False),
            "report": str(self.directory / "report.md"), "credentials": str(self.directory / "credentials.json"),
        })
        lines = ["# " + self.title, "", "- 运行：`" + self.run_id + "`",
                 "- 地址：" + base, "- 结果：" + result["status"],
                 "- 通过：" + str(passed) + "/" + str(len(self.steps)),
                 "- HTTP 请求：" + str(result["http_requests"]), "",
                 "凭据单独保存在同目录 credentials.json（权限 0600，已被 Git 和 Docker 忽略）。", "",
                 "| 测试 | 结果 | 耗时 ms |", "|---|---|---|"]
        for item in self.steps:
            lines.append("| " + item["name"] + " | " + item["status"] + " | " + str(item["duration_ms"]) + " |")
        if failure:
            lines.extend(["", "失败原因：" + self.redact(failure), "", "当次资源保留供排查；流程以非零状态退出。"])
        for item in self.resources.get("external_integrations", []):
            lines.extend(["", "未覆盖：" + item["feature"] + "；原因：" + item["reason"] + "。"])
        (self.directory / "report.md").write_text("\n".join(lines) + "\n")
        print("Report: " + str(self.directory / "report.md"))
        print("Credentials: " + str(self.directory / "credentials.json"))


class Infrastructure:
    def __init__(self, report):
        compose = ["docker", "compose", "--env-file", str(ROOT / ".env.infra"),
                   "-f", str(ROOT / "docker-compose.infra.yaml"), "config", "--format", "json"]
        config = json.loads(subprocess.check_output(compose, stderr=subprocess.PIPE))
        env = config["services"]["admin"]["environment"]
        report.secrets.extend([env["NACOS_PASSWORD"]])
        nacos = os.environ.get("NACOS_PUBLISH_URL", "http://127.0.0.1:8848").rstrip("/")
        login = urllib.request.Request(nacos + "/nacos/v1/auth/users/login", data=urllib.parse.urlencode({
            "username": env["NACOS_USERNAME"], "password": env["NACOS_PASSWORD"],
        }).encode())
        access_token = ""
        try:
            with urllib.request.urlopen(login, timeout=10) as response:
                access_token = json.load(response)["accessToken"]
            query = urllib.parse.urlencode({
                "dataId": "data.yaml", "group": env["NACOS_GROUP"],
                "tenant": env.get("NACOS_NAMESPACE_ID", ""), "accessToken": access_token,
            })
            with urllib.request.urlopen(nacos + "/nacos/v1/cs/configs?" + query, timeout=10) as response:
                document = yaml.safe_load(response.read())
        except urllib.error.HTTPError as error:
            # Nacos URLs contain access tokens; omit the URL from diagnostics.
            report.secrets.append(access_token)
            raise RuntimeError("Nacos bootstrap failed (HTTP " + str(error.code) + ")") from error
        self.redis = document["data"]["redis"]
        self.minio = document.get("oss", {}).get("minio", {})
        self.key = document["runtime"]["crypto_key"].encode().hex()
        report.secrets.extend([self.redis.get("password", ""), document["runtime"]["crypto_key"], self.key])

    def encrypt(self, plain):
        return subprocess.check_output([
            "openssl", "enc", "-aes-128-cbc", "-K", self.key, "-iv", self.key, "-base64", "-A",
        ], input=plain.encode(), stderr=subprocess.PIPE).decode()

    def captcha(self, client):
        item = client.request("GET", "/admin/v1/captcha", authenticated=False)
        answer = subprocess.check_output([
            "docker", "exec", "-e", "REDISCLI_AUTH=" + self.redis.get("password", ""),
            os.environ.get("E2E_REDIS_CONTAINER", "infra-redis"), "redis-cli", "-n",
            str(self.redis.get("db", 0)), "--raw", "GET", "go-admin:captcha:" + item["captchaId"],
        ], text=True, stderr=subprocess.PIPE).strip()
        require(bool(answer), "new CAPTCHA was not found in the configured test Redis DB")
        return {"X-Captcha-Id": item["captchaId"], "X-Captcha-Value": answer.lower()}


class Client:
    def __init__(self, base, report, infra=None):
        self.base, self.report, self.infra = base, report, infra
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
        self.token = None
        self.tenant_code = ""

    def wait_ready(self):
        deadline = time.monotonic() + 30
        last_error = None
        while time.monotonic() < deadline:
            try:
                with self.opener.open(self.base + "/admin/v1/sso/login-info", timeout=3) as response:
                    require(response.status == 200, "unexpected readiness response")
                    return
            except urllib.error.HTTPError as error:
                if error.code not in (500, 502, 503, 504):
                    raise
                last_error = error
            except (urllib.error.URLError, ConnectionError) as error:
                last_error = error
            # Only read-only startup probes retry; business writes never retry.
            time.sleep(0.25)
        raise RuntimeError("API/operator did not become ready within 30 seconds") from last_error

    def request(self, method, path, body=None, expected=200, reason=None, message=None,
                authenticated=True, headers=None, query=None, binary=False):
        values = {"Content-Type": "application/json", "Accept-Language": "zh-CN",
                  "User-Agent": "GoAdminMerchantQA/1.0"}
        if self.tenant_code:
            values["X-Tenant-Code"] = self.tenant_code
        if authenticated and self.token:
            values["Authorization"] = "Bearer " + self.token
        values.update(headers or {})
        # Platform proxies deliberately strip merchant selection. Multi-tenant
        # isolation tests use the real gateway; a single-merchant run may use its frontend.
        base = os.environ.get("E2E_TENANT_BASE_URL", "http://localhost:7788").rstrip("/") if self.tenant_code else self.base
        url = base + path
        if query:
            url += "?" + urllib.parse.urlencode(query)
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(url, data=data, method=method, headers=values)
        start = time.monotonic()
        try:
            with self.opener.open(req, timeout=15) as response:
                status, raw = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, raw = error.code, error.read()
        allowed = (expected,) if isinstance(expected, int) else expected
        record = {
            "method": method, "path": path, "status": status,
            "duration_ms": round((time.monotonic() - start) * 1000),
            "reason": None, "expected_statuses": list(allowed),
        }
        if self.report.active is not None:
            self.report.active["requests"].append(record)
        if binary:
            require(status in allowed, method + " " + path + ": HTTP " + str(status) + ", expected " + str(allowed))
            return raw
        try:
            payload = json.loads(raw) if raw else {}
        except json.JSONDecodeError as error:
            raise AssertionError(method + " " + path + ": response is not one JSON document (HTTP " + str(status) + ")") from error
        require(isinstance(payload, dict), path + ": expected a JSON object")
        record["reason"] = payload.get("reason")
        require(status in allowed, method + " " + path + ": HTTP " + str(status) +
                ", expected " + str(allowed) + "; " + payload.get("reason", "") + " " + payload.get("message", ""))
        if reason:
            require(payload.get("reason") == reason, path + ": unexpected error reason " + str(payload.get("reason")))
        if message:
            require(message in payload.get("message", ""), path + ": unexpected error message " + str(payload.get("message")))
        return payload

    def login(self, username, plain, tenant_code="", expected=200, reason=None, captcha=None):
        captcha_headers = captcha if captcha is not None else self.infra.captcha(self)
        self.tenant_code = tenant_code
        payload = self.request("POST", "/admin/v1/login", {
            "grant_type": "password", "username": username,
            "password": self.infra.encrypt(plain),
        }, expected=expected, reason=reason, authenticated=False,
            headers=captcha_headers)
        if expected == 200:
            self.token = payload.get("access_token")
            require(bool(self.token), "login did not return an access token")
            self.report.secrets.append(self.token)
            cookie_name = "refresh_token" + ("_" + tenant_code if tenant_code else "")
            require(any(cookie.name == cookie_name and cookie.has_nonstandard_attr("HttpOnly")
                        for cookie in self.cookies), "login did not issue an HttpOnly refresh cookie")
        return payload

    def find(self, resource, field, value, filters=None):
        query = dict(filters or {})
        query[field] = value
        payload = self.request("GET", "/admin/v1/" + resource, query={
            "query": json.dumps(query), "noPaging": "true",
        })
        matches = [item for item in payload.get("items", []) if item.get(field) == value]
        require(len(matches) == 1, resource + ": expected exactly one matching " + field)
        return matches[0]

    def update(self, resource, identifier, data):
        return self.request("PUT", "/admin/v1/" + resource + "/" + str(identifier), {
            "data": data, "updateMask": ",".join(data),
        })


def ordered_menus(items):
    result = []
    for item in items:
        node = dict(item)
        if "children" in node:
            node["children"] = ordered_menus(node["children"])
        result.append(node)
    return sorted(result, key=lambda node: (node.get("name", ""), node.get("path", "")))


def menu_nodes(items):
    for item in items:
        yield item
        yield from menu_nodes(item.get("children", []))


class Lifecycle:
    def __init__(self, report, infra=None):
        self.report = report
        self.base = os.environ.get("E2E_BASE_URL", "http://localhost:15888").rstrip("/")
        self.primary = None
        self.infra = infra
        self.admin = None
        self.username = "owner_" + report.run_id[-6:]
        self.initial_password, self.final_password, self.other_password = password(), password(), password()
        self.report.secrets.extend([self.initial_password, self.final_password, self.other_password])
        self.current_password = self.initial_password
        self.tenants = []

    def new_client(self):
        return Client(self.base, self.report, self.infra)

    def create_tenant(self, suffix, plain):
        code = "E2e" + secrets.token_hex(14) + "A"
        tenant = {"name": "自动化租户_" + suffix + "_" + self.report.run_id, "code": code,
                  "type": "TRIAL", "status": "ON", "auditStatus": "APPROVED", "expiredAt": timestamp(30)}
        self.admin.request("POST", "/admin/v1/tenants:with-admin", {
            "tenant": tenant, "user": {"username": self.username}, "password": plain,
        })
        row = self.admin.find("tenants", "code", code)
        self.tenants.append(row)
        self.report.resources["tenants"].append({"id": row["id"], "code": code, "retained": True})
        return row

    def assert_profile(self, client, tenant):
        me = client.request("GET", "/admin/v1/me")
        require(me.get("tenantId") == tenant["id"] and me.get("username") == self.username,
                "profile crossed a tenant boundary")
        require(me.get("id") == tenant["adminUserId"], "tenant administrator link does not match login identity")
        return me

    def run(self):
        step = self.report.step
        with step("平台登录与未认证门禁"):
            self.infra = self.infra or Infrastructure(self.report)
            self.admin = self.new_client()
            self.admin.request("GET", "/admin/v1/me", expected=401, authenticated=False)
            platform_password = os.environ.get("ADMIN_SMOKE_PASSWORD", "Abcd@1234")
            self.report.secrets.append(platform_password)
            self.admin.login(os.environ.get("ADMIN_SMOKE_USERNAME", "admin"), platform_password)
            require(self.admin.request("GET", "/admin/v1/me").get("tenantId", 0) == 0,
                    "provisioning account must be a platform account")

        with step("创建租户及管理员、更新和查询"):
            self.primary = self.create_tenant("primary", self.initial_password)
            self.admin.update("tenants", self.primary["id"], {"remark": "core framework regression"})
            require(self.admin.request("GET", "/admin/v1/tenants/" + str(self.primary["id"])).get("remark")
                    == "core framework regression", "tenant update did not persist")

        owner = self.new_client()
        with step("商户登录、菜单及权限初始化"):
            owner.login(self.username, self.current_password, self.primary["code"])
            self.assert_profile(owner, self.primary)
            initial = owner.request("GET", "/admin/v1/initial-context")
            require(initial.get("permissions"), "tenant administrator has no permissions")
            require(initial.get("menus"), "tenant administrator has no menus")
            owner.request("GET", "/admin/v1/tenants", expected=403)

        with step("同名账号的租户隔离"):
            other = self.create_tenant("isolation", self.other_password)
            second = self.new_client()
            second.login(self.username, self.other_password, other["code"])
            self.assert_profile(second, other)
            owner.request("GET", "/admin/v1/users/" + str(other["adminUserId"]), expected=404)
            second.request("GET", "/admin/v1/users/" + str(self.primary["adminUserId"]), expected=404)
            second.request("POST", "/admin/v1/logout", {})

        with step("停用与到期租户门禁，恢复后可继续访问"):
            for values in [{"status": "OFF"}, {"expiredAt": timestamp(-1)}]:
                self.admin.update("tenants", self.primary["id"], values)
                try:
                    owner.request("GET", "/admin/v1/me", expected=403)
                finally:
                    self.admin.update("tenants", self.primary["id"], {"status": "ON", "expiredAt": timestamp(30)})
                self.assert_profile(owner, self.primary)

        with step("刷新会话、退出及令牌撤销"):
            refreshed = owner.request("POST", "/admin/v1/refresh-token", {"grant_type": "refresh_token", "client_type": "admin"})
            owner.token = refreshed["access_token"]
            self.report.secrets.append(owner.token)
            self.assert_profile(owner, self.primary)
            owner.request("POST", "/admin/v1/logout", {})
            owner.request("GET", "/admin/v1/me", expected=401)
            owner.login(self.username, self.current_password, self.primary["code"])
            self.assert_profile(owner, self.primary)
            owner.request("POST", "/admin/v1/logout", {})
            self.report.ready = True

    def credentials(self):
        return {
            "login_url": os.environ.get("E2E_MERCHANT_URL", "http://localhost:15889").rstrip("/") + "/auth/login?redirect=%2F", "ready": self.report.ready,
            "tenant_code": self.primary["code"] if self.primary else None,
            "tenant_id": self.primary["id"] if self.primary else None,
            "username": self.username, "password": self.current_password,
        }


def main():
    report = Report()
    flow = Lifecycle(report)
    failure = None
    try:
        flow.run()
    except Exception as error:
        failure = report.redact(error)
        report.failure_trace = report.redact(traceback.format_exc())
        print("FAIL: " + failure, file=sys.stderr)
    finally:
        report.save(flow.base, flow.credentials(), failure)
    return 1 if failure else 0


if __name__ == "__main__":
    sys.exit(main())
