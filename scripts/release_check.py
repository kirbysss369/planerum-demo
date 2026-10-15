"""Verify the local demo through HTTP; save evidence against the release plan.

Run only against a server started with DEMO_MODE=1. This checks transport,
workflow and file consistency, not the quality or speed of a Cloud.ru model.
"""

import argparse
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import time
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from xml.etree import ElementTree as ET
from zipfile import ZipFile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="http://127.0.0.1:18080")
    parser.add_argument("--demo", action="store_true", required=True,
                        help="confirm that the target is an isolated DEMO_MODE=1 server")
    parser.add_argument("--out", default="data/release-check/evidence")
    parser.add_argument("--reference", action="append", default=[], help="additional local DOCX/PDF/TXT to upload as test data")
    args = parser.parse_args()
    root = Path(args.out)
    root.mkdir(parents=True, exist_ok=True)
    token = os.environ.get("SERVICE_TOKEN", "")
    results = {"mode": "demo", "technical": [], "scenarios": [], "observations": []}

    def save(name, data):
        (root / name).write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")

    def http(path, data=None, authorized=True, content_type="application/json"):
        headers = {"Content-Type": content_type}
        if authorized:
            headers["Authorization"] = "Bearer " + token
        if data is not None and not isinstance(data, bytes):
            data = json.dumps(data, ensure_ascii=False).encode()
        req = Request(args.base.rstrip("/") + path, data=data, headers=headers)
        try:
            response = urlopen(req, timeout=60)
        except HTTPError as error:
            response = error
        with response:
            raw = response.read()
            status = response.code
            if "application/json" in response.headers.get("Content-Type", ""):
                raw = json.loads(raw)
            return status, raw

    def check(name, ok, evidence):
        results["technical"].append({"name": name, "passed": bool(ok), "evidence": evidence})
        if not ok:
            save("results.json", results)
            raise AssertionError(f"{name}: {evidence}")

    def agent(action, expected=200, **fields):
        status, data = http("/v1/agent", {"action": action, **fields})
        if status != expected:
            raise AssertionError(f"{action}: expected HTTP {expected}, got {status}: {data}")
        return data

    def generate(p):
        queued = agent("generate_async", expected=202, program_id=p["id"], revision=p["revision"])
        job = queued["job"]
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            status = agent("job_status", job_id=job["id"])
            if status["job"]["state"] == "done":
                return status["program"], status["job"]
            if status["job"]["state"] == "failed":
                raise AssertionError(status)
            time.sleep(0.2)
        raise AssertionError("demo job did not finish within 30 seconds")

    def export(p, folder):
        response = agent("export", program_id=p["id"], revision=p["revision"])
        required = {"program.json", "program.docx", "lectures.pptx", "tests.xml", "validation-report.json"}
        check(folder + ": artifact set", set(response["artifacts"]) == required, sorted(response["artifacts"]))
        out = root / folder
        out.mkdir(exist_ok=True)
        files = {}
        for name, path in response["artifacts"].items():
            status, raw = http(path)
            check(folder + ": download " + name, status == 200 and bool(raw), {"http": status})
            if isinstance(raw, dict):
                raw = json.dumps(raw, ensure_ascii=False, indent=2).encode()
            files[name] = raw
            (out / name).write_bytes(raw)
            unauthorized, _ = http(path, authorized=False)
            check(folder + ": protected " + name, unauthorized == 401, unauthorized)
        exported = json.loads(files["program.json"])
        check(folder + ": JSON revision", exported == p, p["revision"])
        report = json.loads(files["validation-report.json"])
        check(folder + ": report", report["revision"] == p["revision"] and report["structurally_valid"]
              and report["methodical_review_required"], report)
        questions = ET.fromstring(files["tests.xml"]).findall("question")
        check(folder + ": LMS question count", len(questions) == len(p["assessments"]), len(questions))
        for a, q in zip(p["assessments"], questions):
            check(folder + ": LMS question " + a["id"], q.findtext("name/text") == a["id"]
                  and q.findtext("questiontext/text") == a["question"]
                  and (a["kind"] != "test" or q.findtext("answer/text") == a["answer"]), a["id"])
        for name in ("program.docx", "lectures.pptx"):
            with ZipFile(io.BytesIO(files[name])) as z:
                check(folder + ": ZIP " + name, z.testzip() is None, len(z.namelist()))
                props = ET.fromstring(z.read("docProps/core.xml"))
                ns = {"dc": "http://purl.org/dc/elements/1.1/", "cp": "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"}
                check(folder + ": metadata " + name, props.findtext("dc:title", namespaces=ns) == p["spec"]["title"]
                      and not props.findtext("dc:creator", namespaces=ns)
                      and not props.findtext("dc:description", namespaces=ns)
                      and not props.findtext("cp:lastModifiedBy", namespaces=ns), "title matches; no default authors")
        return report

    health, raw = http("/healthz")
    check("health", health == 200 and raw.strip() == b"ok", health)
    status, _ = http("/v1/agent", {"action": "status", "program_id": "missing"}, authorized=False)
    check("API requires token", status == 401, status)
    for body in (b"null", b"{", b'{"action":"status","unexpected":1}', b'{"action":"status"} {}'):
        status, _ = http("/v1/agent", body)
        check("malformed request: " + body.decode(), status == 400, status)
    check("unknown program", http("/v1/agent", {"action": "status", "program_id": "missing"})[0] == 404, "HTTP 404")
    free_text_status, _ = http("/v1/agent", {"action": "create", "prompt": "Демонстрационный запрос по кибербезопасности"})
    results["observations"].append({"name": "free text request", "http": free_text_status,
                                    "supported": free_text_status != 400})

    incomplete = agent("create", spec={"title": "Демонстрационный курс по кибербезопасности", "audience": "Студенты колледжа", "requirements": "Примерно месяц"})
    save("clarification.json", incomplete)
    check("clarifications before outline", len(incomplete.get("questions", [])) == 5 and "program" not in incomplete,
          incomplete)

    scenarios = [
        ("python16", "general", 16, False, "Школьники 8 и 9 классов", "Нет опыта программирования", "Очно", "Защита небольшого проекта",
         "8 занятий по 2 часа, два раза в неделю, 4 недели; практика не менее 8 часов; ровно 10 вопросов с ключами; слайды занятий 1 и 2"),
        ("teachers36", "qualification", 36, False, "Учителя школ Ингушетии", "Без навыков программирования", "Очно-заочно с ДОТ", "Зачёт по портфолио из трёх материалов",
         "Планы уроков, задания и проверочные работы; риски, этика и защита данных детей; сервисы, доступные в России; профстандарт Педагог"),
        ("bank72", "qualification", 72, False, "Сотрудники банков", "Excel, без опыта Python", "Очно-заочно", "Защита проекта на обезличенных банковских данных",
         "4 недели; основы Python, pandas, визуализация, SQL, статистика; слайды для всех лекций модуля pandas"),
        ("cyber72", "qualification", 72, False, "Студенты 3 курса ИСиП, получают СПО", "Основы сетей и Linux", "Очно на базе колледжа", "Экзамен с практическим заданием на стенде",
         "Около месяца; начальный уровень; сообщить расхождение с обещанием полной подготовки к работе"),
        ("devops256", "qualification", 256, True, "Стажёры ИТ-компаний региона", "Начальный", "Самостоятельно под руководством наставника", "Защита отчёта о стажировке",
         "254 часа СРС, 2 часа ИА, 0 контактных; около двух месяцев, не более 4 часов в день; Linux, сети, контейнеры, CI/CD, мониторинг, IaC; вопросы наставника"),
        ("boundary500", "qualification", 500, False, "Начинающие", "Начальный", "Очно-заочно", "Защита проекта", "Проверка верхней границы объёма"),
        ("fractional72_5", "general", 72.5, False, "Начинающие", "Начальный", "Очно", "Защита проекта", "Проверка дробного объёма"),
    ]
    programs = {}
    for name, kind, hours, independent, audience, level, delivery, attestation, requirements in scenarios:
        spec = dict(title="Демонстрационная проверка: " + name, kind=kind, requested=hours,
                    independent_internship=independent, audience=audience, entry_level=level,
                    delivery=delivery, attestation=attestation, requirements=requirements)
        save(name + "-request.json", {"action": "create", "spec": spec})
        started = time.monotonic()
        p = agent("answer" if name == "cyber72" else "create", spec=spec)["program"]
        check(name + ": fixture", "ДЕМОНСТРАЦИОННЫЙ" in p["goal"], p["goal"])
        check(name + ": outline", p["state"] == "outline_ready" and not any(i["severity"] == "error" for i in p.get("issues", [])), p["state"])
        agent("generate", expected=422, program_id=p["id"], revision=p["revision"])
        agent("generate_async", expected=422, program_id=p["id"], revision=p["revision"])
        agent("export", expected=422, program_id=p["id"], revision=p["revision"])
        check(name + ": approval guard", True, "generation and export rejected before approval")
        p = agent("approve_outline", program_id=p["id"], revision=p["revision"])["program"]
        p, job = generate(p)
        check(name + ": queue", p["state"] == "ready" and job["attempts"] == 1, job)
        report = export(p, name)
        distribution = report["hours"]
        total = sum(distribution[k] for k in ("lectures", "practice", "laboratory", "self_study", "final_attestation"))
        check(name + ": exact hours", abs(total - hours) < 1e-9, distribution)
        count = sum(a["kind"] == "test" for a in p["assessments"])
        criteria = {"fixture_only": True, "test_count": count, "calendar_present": "calendar" in p}
        if name == "python16":
            criteria.update(practice_minimum_satisfied=distribution["practice"] >= 8, ten_tests_satisfied=count == 10)
        if independent:
            check(name + ": internship hours", distribution["self_study"] == 254 and distribution["final_attestation"] == 2
                  and sum(distribution[k] for k in ("lectures", "practice", "laboratory")) == 0, distribution)
            criteria["minimum_days_at_4_hours"] = 64
        results["scenarios"].append({"name": name, "program_id": p["id"], "revision": p["revision"], "technical_passed": True,
                                     "demo_seconds": round(time.monotonic() - started, 3), "acceptance": "not_confirmed", "criteria": criteria})
        programs[name] = p

    references = [("demo-reference.txt", "Демонстрационный референс: 72 часа, защита проекта.".encode()),
                  ("demo-reference.docx", (root / "python16" / "program.docx").read_bytes())]
    references.extend((Path(path).name, Path(path).read_bytes()) for path in args.reference)
    for name, raw in references:
        boundary = "planerum-local-release-boundary"
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\n'
                'Content-Type: application/octet-stream\r\n\r\n').encode() + raw + f'\r\n--{boundary}--\r\n'.encode()
        status, data = http("/v1/references", body, content_type="multipart/form-data; boundary=" + boundary)
        check("reference " + name, status == 200 and data.get("sha256") == hashlib.sha256(raw).hexdigest()
              and data.get("bytes") == len(raw) and bool(data.get("text", "").strip()),
              {"http": status, "characters": len(data.get("text", "")), "sha256": data.get("sha256")})

    # The first result claims two questions that now assess only the second.
    # This must invalidate readiness and the exported coverage report.
    p = programs["teachers36"]
    proposed = copy.deepcopy(p)
    second = copy.deepcopy(p["outcomes"][0])
    second["id"], second["text"] = "outcome-2", "Второй демонстрационный результат"
    proposed["outcomes"].append(second)
    p = agent("revise", program_id=p["id"], revision=p["revision"], program=proposed)["program"]
    p = agent("approve_outline", program_id=p["id"], revision=p["revision"])["program"]
    p, _ = generate(p)
    proposed = copy.deepcopy(p)
    for assessment in proposed["assessments"]:
        assessment["outcome_ids"] = ["outcome-2"]
    proposed["outcomes"][1]["assessment_ids"] = [a["id"] for a in proposed["assessments"]]
    response = agent("revise", program_id=p["id"], revision=p["revision"], program=proposed)
    save("reciprocal-after-fix.json", response)
    p = response["program"]
    check("one-sided coverage rejected", p["state"] != "ready" and any(i["severity"] == "error" for i in response.get("issues", [])), response.get("issues"))
    agent("export", expected=422, program_id=p["id"], revision=p["revision"])
    check("one-sided coverage cannot be exported", True, "HTTP 422")

    p = programs["bank72"]
    original = copy.deepcopy(p)
    proposed = copy.deepcopy(p)
    proposed["assessments"][0]["question"] += " (ручная правка)"
    p = agent("revise", program_id=p["id"], revision=p["revision"], program=proposed)["program"]
    check("material edit preserves outline and other content", p["state"] == "ready" and p["approved_revision"] == original["approved_revision"]
          and p["lectures"] == original["lectures"] and p["assessments"][1:] == original["assessments"][1:], p["revision"])
    agent("revise", expected=409, program_id=p["id"], revision=original["revision"], program=proposed)
    check("stale revision rejected", True, "HTTP 409")
    proposed = copy.deepcopy(p)
    proposed["modules"][0]["topics"][0]["title"] += " (замена темы)"
    p = agent("revise", program_id=p["id"], revision=p["revision"], program=proposed)["program"]
    check("structural edit revokes approval", p["approved_revision"] == 0 and p["state"] == "outline_ready", p["state"])
    results["observations"].append({"name": "topic edit clears all materials", "assessments_after": len(p.get("assessments", [])),
                                    "lectures_after": len(p.get("lectures", [])), "selective_regeneration": "not_implemented"})
    save("results.json", results)
    print(f"Technical checks: {len(results['technical'])} passed; scenarios: {len(results['scenarios'])} demo workflows; educational acceptance: not confirmed")
    print(root / "results.json")


if __name__ == "__main__":
    main()
