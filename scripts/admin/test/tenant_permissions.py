"""Exercise configured RBAC, field redaction and live revocation on a new E2E tenant."""

import json
import os
from pathlib import Path
import sys
import traceback

from tenant_lifecycle import Client, Infrastructure, Report, ROOT, menu_nodes, password, require


def run(report, credentials, infra=None):
    infra = infra or Infrastructure(report)
    base = os.environ.get("E2E_BASE_URL", "http://localhost:15888").rstrip("/")
    admin = Client(base, report, infra)
    owner = Client(base, report, infra)
    report.secrets.append(credentials["password"])
    reader_password = password()
    report.secrets.append(reader_password)
    suffix = report.run_id
    tenant_id = credentials["tenant_id"]
    step = report.step

    with step("平台与新租户管理员真实登录"):
        admin_password = os.environ.get("ADMIN_SMOKE_PASSWORD", "Abcd@1234")
        report.secrets.append(admin_password)
        admin.login(os.environ.get("ADMIN_SMOKE_USERNAME", "admin"), admin_password)
        owner.login(credentials["username"], credentials["password"], credentials["tenant_code"])
        require(owner.request("GET", "/admin/v1/me")["tenantId"] == tenant_id, "wrong tenant identity")

    with step("创建权限组、只读权限点并关联真实用户接口与菜单"):
        group_name = "流程验收权限组_" + suffix
        admin.request("POST", "/admin/v1/permission-groups", {"data": {
            "name": group_name, "module": "OPM", "status": "ON",
        }})
        group = admin.find("permission-groups", "name", group_name)
        admin.update("permission-groups", group["id"], {"description": "新流程权限管理验收"})
        apis = admin.request("GET", "/admin/v1/apis", query={"noPaging": "true"}).get("items", [])
        read_apis = [item["id"] for item in apis if item.get("method") == "GET" and
                     item.get("path") in ("/admin/v1/users", "/admin/v1/users/{id}")]
        require(len(read_apis) == 2, "user list/detail APIs must be synchronized before testing")
        menus = admin.request("GET", "/admin/v1/menus", query={"noPaging": "true"}).get("items", [])
        user_menus = [item["id"] for item in menu_nodes(menus) if item.get("component", "").startswith("app/opm/user")]
        require(bool(user_menus), "user menu is missing")
        code = "qa:users:view:" + suffix
        admin.request("POST", "/admin/v1/permissions", {"data": {
            "name": "验收用户只读权限_" + suffix, "code": code, "status": "ON",
            "groupId": group["id"], "apiIds": read_apis, "menuIds": user_menus,
        }})
        permission = admin.find("permissions", "code", code)
        detail = admin.request("GET", "/admin/v1/permissions/" + str(permission["id"]))
        require(set(detail.get("apiIds", [])) == set(read_apis), "permission API binding mismatch")
        require(set(detail.get("menuIds", [])) == set(user_menus), "permission menu binding mismatch")
        admin.update("permissions", permission["id"], {"description": "仅允许读取本租户用户，禁止写入"})
        base_permission = admin.find("permissions", "code", "sys:access_backend")
        granted = [base_permission["id"], permission["id"]]
        report.resources["permission_groups"] = [group["id"]]
        report.resources["permissions"] = [permission["id"]]

    with step("配置租户管理员可分配权限，并验证不能授予平台权限"):
        owner_user = admin.request("GET", "/admin/v1/users/" + str(owner.request("GET", "/admin/v1/me")["id"]))
        require(bool(owner_user.get("roleIds")), "tenant administrator has no assigned role")
        owner_role = admin.request("GET", "/admin/v1/roles/" + str(owner_user["roleIds"][0]))
        owner_permissions = list(dict.fromkeys(owner_role.get("permissions", []) + [permission["id"]]))
        admin.update("roles", owner_role["id"], {"permissions": owner_permissions})
        platform_permission = admin.find("permissions", "code", "sys:platform_admin")
        owner.request("POST", "/admin/v1/roles", {"data": {
            "name": "禁止的提权角色_" + suffix, "code": "tenant:forbidden:" + suffix,
            "permissions": [platform_permission["id"]],
        }}, expected=403)

    with step("租户管理员创建只读角色、字段权限与普通成员"):
        role_code = "tenant:qa_reader:" + suffix
        hidden = [{"resource": "User", "hiddenFields": ["email", "mobile"]}]
        owner.request("POST", "/admin/v1/roles", {"data": {
            "name": "流程验收只读角色_" + suffix, "code": role_code, "type": "TENANT",
            "tenantId": tenant_id, "status": "ON", "dataScope": "ALL",
            "permissions": granted, "fieldPermissions": hidden,
        }})
        roles = admin.request("GET", "/admin/v1/roles", query={"noPaging": "true"}).get("items", [])
        role = next(item for item in roles if item.get("name") == "流程验收只读角色_" + suffix)
        role_id = role["id"]
        report.resources["roles"] = [role_id]
        detail = admin.request("GET", "/admin/v1/roles/" + str(role_id))
        require(detail.get("tenantId") == tenant_id and set(detail.get("permissions", [])) == set(granted),
                "role tenant/permission binding mismatch")
        reader_name = "reader_" + report.run_id[-6:]
        email = reader_name + "@example.test"
        owner.request("POST", "/admin/v1/users", {"data": {
            "username": reader_name, "nickname": "流程验收只读成员", "email": email,
            "roleIds": [role_id], "status": "NORMAL",
        }, "password": reader_password})
        member = owner.find("users", "username", reader_name)
        report.resources["users"] = [member["id"]]
        require(member.get("tenantId") == tenant_id, "reader was created in another tenant")
        visible_email = owner.request("GET", "/admin/v1/users/" + str(member["id"])).get("email")
        require(bool(visible_email) and visible_email.endswith("@example.test"),
                "baseline email is missing; field hiding could not be meaningfully tested")
        credentials["reader"] = {"username": reader_name, "password": reader_password,
                                  "role_id": role_id, "user_id": member["id"]}
        report.resources["roles"] = [role_id]
        report.resources["users"] = [member["id"]]

    reader = Client(base, report, infra)
    with step("普通成员登录、用户读取成功、越权写入与角色管理被拒"):
        reader.login(reader_name, reader_password, credentials["tenant_code"])
        rows = reader.request("GET", "/admin/v1/users", query={"noPaging": "true"}).get("items", [])
        require(bool(rows) and all(item.get("tenantId") == tenant_id for item in rows), "reader crossed tenant boundary")
        reader.request("POST", "/admin/v1/users", {"data": {"username": "forbidden_" + suffix},
                                                    "password": password()}, expected=403)
        reader.request("GET", "/admin/v1/roles", expected=403)
        reader.request("POST", "/admin/v1/permissions", {"data": {"code": "forbidden_" + suffix}}, expected=403)
        codes = reader.request("GET", "/admin/v1/perm-codes")
        require(code in codes.get("codes", []), "custom permission code missing")
        api_grants = {(item.get("method"), item.get("path")) for item in codes.get("apiPermissions", [])}
        require(("GET", "/admin/v1/users") in api_grants, "effective read grant missing from UI authorization facts")
        require(("POST", "/admin/v1/users") not in api_grants, "read-only role exposed a create-user UI grant")
        initial = reader.request("GET", "/admin/v1/initial-context")
        require(api_grants == {(item.get("method"), item.get("path")) for item in initial.get("apiPermissions", [])},
                "initial API authorization facts mismatch")
        require({"User.email", "User.mobile"}.issubset(codes.get("hiddenFields", [])), "hidden field contract missing")
        detail = reader.request("GET", "/admin/v1/users/" + str(member["id"]))
        require(not detail.get("email"), "hidden email leaked through the HTTP response")
        menus = reader.request("GET", "/admin/v1/routes").get("items", [])
        require(any(item.get("component", "").startswith("app/opm/user") for item in menu_nodes(menus)), "authorized user menu missing")

    with step("角色撤权在同一登录会话立即生效，恢复后接口与菜单恢复"):
        admin.update("roles", role_id, {"permissions": [base_permission["id"]]})
        try:
            reader.request("GET", "/admin/v1/users", expected=403)
            revoked = reader.request("GET", "/admin/v1/perm-codes")
            require(code not in revoked.get("codes", []), "revoked code remained")
            require(not any(item.get("path") == "/admin/v1/users" for item in revoked.get("apiPermissions", [])),
                    "revoked API grant remained in UI authorization facts")
            menus = reader.request("GET", "/admin/v1/routes").get("items", [])
            require(not any(item.get("component", "").startswith("app/opm/user") for item in menu_nodes(menus)), "revoked menu remained")
        finally:
            admin.update("roles", role_id, {"permissions": granted})
        reader.request("GET", "/admin/v1/users")

    with step("字段权限变更吊销旧会话，重新登录按最新配置隐藏字段"):
        admin.update("roles", role_id, {"fieldPermissions": []})
        try:
            reader.request("GET", "/admin/v1/users", expected=401)
            reader.login(reader_name, reader_password, credentials["tenant_code"])
            detail = reader.request("GET", "/admin/v1/users/" + str(member["id"]))
            require(detail.get("email") == visible_email, "removing field restriction did not restore the baseline masked value")
        finally:
            admin.update("roles", role_id, {"fieldPermissions": hidden})
        reader.request("GET", "/admin/v1/users", expected=401)
        reader.login(reader_name, reader_password, credentials["tenant_code"])
        require(not reader.request("GET", "/admin/v1/users/" + str(member["id"])).get("email"), "restored redaction failed")

    with step("禁用权限点阻断访问，启用后恢复"):
        admin.update("permissions", permission["id"], {"status": "OFF"})
        try:
            reader.request("GET", "/admin/v1/users", expected=403)
            disabled = reader.request("GET", "/admin/v1/perm-codes")
            require(not any(item.get("path") == "/admin/v1/users" for item in disabled.get("apiPermissions", [])),
                    "disabled permission remained in UI authorization facts")
        finally:
            admin.update("permissions", permission["id"], {"status": "ON"})
        reader.request("GET", "/admin/v1/users")

    with step("禁用角色阻断访问，启用后恢复"):
        admin.update("roles", role_id, {"status": "OFF"})
        try:
            reader.request("GET", "/admin/v1/users", expected=(401, 403))
        finally:
            admin.update("roles", role_id, {"status": "ON"})
        reader.login(reader_name, reader_password, credentials["tenant_code"])
        reader.request("GET", "/admin/v1/users")

    with step("修改成员角色使原会话失权，重新登录遵循新角色"):
        temporary_name = "验收临时基础角色_" + suffix
        admin.request("POST", "/admin/v1/roles", {"data": {
            "name": temporary_name, "code": "tenant:qa_base:" + suffix,
            "tenantId": tenant_id, "type": "TENANT", "status": "ON",
            "permissions": [base_permission["id"]],
        }})
        temporary = admin.find("roles", "name", temporary_name)
        owner.update("users", member["id"], {"roleIds": [temporary["id"]]})
        try:
            reader.request("GET", "/admin/v1/users", expected=(401, 403))
            reader.login(reader_name, reader_password, credentials["tenant_code"])
            reader.request("GET", "/admin/v1/users", expected=403)
        finally:
            owner.update("users", member["id"], {"roleIds": [role_id]})
            admin.request("DELETE", "/admin/v1/roles/" + str(temporary["id"]))
        reader.login(reader_name, reader_password, credentials["tenant_code"])
        reader.request("GET", "/admin/v1/users")

    with step("禁用成员即时阻断旧会话与登录，启用后恢复"):
        owner.update("users", member["id"], {"status": "DISABLED", "roleIds": [role_id]})
        try:
            reader.request("GET", "/admin/v1/users", expected=(401, 403))
            reader.login(reader_name, reader_password, credentials["tenant_code"], expected=(400, 403))
        finally:
            owner.update("users", member["id"], {"status": "NORMAL", "roleIds": [role_id]})
        reader.login(reader_name, reader_password, credentials["tenant_code"])
        reader.request("GET", "/admin/v1/users")

    with step("普通成员退出、重新登录及最终授权验收"):
        reader.request("POST", "/admin/v1/logout", {})
        reader.request("GET", "/admin/v1/users", expected=401)
        reader.login(reader_name, reader_password, credentials["tenant_code"])
        reader.request("GET", "/admin/v1/users")
        reader.request("POST", "/admin/v1/logout", {})
        owner.request("POST", "/admin/v1/logout", {})

    with step("确认保留的验收租户可用状态"):
        tenant_name = "Go Admin 验收租户 " + report.run_id[-6:]
        admin.update("tenants", tenant_id, {"name": tenant_name})
        require(admin.request("GET", "/admin/v1/tenants/" + str(tenant_id)).get("status") == "ON",
                "retained tenant must remain enabled")
        credentials["tenant_name"] = tenant_name
        report.ready = True
        credentials["ready"] = True
        credentials["reader"]["ready"] = True


def main():
    latest = json.loads((ROOT / ".cache/admin-artifacts/tenant-e2e/latest.json").read_text())
    source = Path(os.environ.get("TENANT_E2E_CREDENTIALS", latest["credentials"]))
    credentials = json.loads(source.read_text())
    require(credentials.get("ready"), "complete tenant_lifecycle.py successfully first")
    report = Report(ROOT / ".cache/admin-artifacts/tenant-permissions")
    report.secrets.append(credentials["password"])
    report.resources["tenants"] = [{"id": credentials["tenant_id"], "retained": True}]
    failure = None
    try:
        run(report, credentials)
    except Exception as error:
        failure = report.redact(error)
        report.failure_trace = report.redact(traceback.format_exc())
        print("FAIL: " + failure, file=sys.stderr)
    finally:
        credentials["ready"] = report.ready
        report.save(os.environ.get("E2E_BASE_URL", "http://localhost:15888"), credentials, failure)
    return 1 if failure else 0


if __name__ == "__main__":
    sys.exit(main())
