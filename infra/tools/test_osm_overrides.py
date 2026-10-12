"""Deterministic checks for osm_overrides.py (SPEC §4.3). Run with the osmium env:
    python infra/tools/test_osm_overrides.py
"""
import json
import os
import subprocess
import sys
import tempfile

import osmium

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
FIXTURE = os.path.join(ROOT, "tests", "fixtures", "overrides-cases.osm")
SCRIPT = os.path.join(ROOT, "infra", "tools", "osm_overrides.py")


def tags_by_way(path):
    out = {}
    for w in osmium.FileProcessor(path, osmium.osm.WAY):
        out[w.id] = dict(w.tags)
    return out


def run(country, rules=None):
    tmp = tempfile.mkdtemp()
    out = os.path.join(tmp, "out.osm.pbf")
    args = [sys.executable, SCRIPT, "--country", country, "--in", FIXTURE, "--out", out, "--report", os.path.join(tmp, "r.json")]
    if rules:
        rp = os.path.join(tmp, "rules.json")
        with open(rp, "w", encoding="utf-8") as f:
            json.dump(rules, f)
        args += ["--rules", rp]
    subprocess.run(args, check=True, capture_output=True)
    return tags_by_way(out)


def main():
    rules = [{"id": "mumbai_pune_expressway", "name_patterns": ["mumbai.?pune expressway"], "highways": ["motorway", "trunk"], "bbox": [73.0, 18.55, 73.85, 19.05]}]
    ind = run("in", rules)
    assert ind[10].get("motor_vehicle") == "no" and ind[10].get("motorcar") == "no", ind[10]
    assert "motor_vehicle" not in ind[11], "service road must stay open"
    assert "motor_vehicle" not in ind[12], "a same-named way outside the bbox must stay open"
    assert ind[13].get("motor_vehicle") == "no", "motorcycle=no must close the way to two-wheelers"
    se = run("se")
    assert se[20].get("motor_vehicle") == "no", "unmarked track is closed to mopeds"
    assert se[21].get("motor_vehicle") == "destination", "explicit motor access is kept"
    print("osm_overrides: all cases pass")


if __name__ == "__main__":
    main()
