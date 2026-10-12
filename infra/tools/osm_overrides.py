"""Inject regional access rules into an OSM extract (SPEC §4.3).

India (two-wheeler graph):
  * ways matching data/overrides/in_2w_restrictions.yaml (name regex + highway + bbox)
    get motor_vehicle=no (and motorcar=no, which GraphHopper's car parser reads first);
  * ways tagged motorcycle=no get the same, because car_access ignores `motorcycle`.
Sweden (bicycle/moped graph):
  * highway=track without explicit motor access gets motor_vehicle=no, so moped
    profiles (car_access) skip it while bicycle profiles (bike_access) still use it.

Run with the osmium conda env:  python osm_overrides.py --country in --in X.pbf --out Y.pbf --rules rules.json --report r.json
"""
import argparse
import json
import re
import sys

import osmium

MOTOR_KEYS = ("motor_vehicle", "motorcar", "motorcycle", "moped", "mofa", "vehicle", "access")
MOTOR_OK = {"yes", "designated", "permissive", "destination", "agricultural", "forestry", "delivery", "customers"}


def load_rules(path):
    with open(path, encoding="utf-8") as f:
        rules = json.load(f)
    for r in rules:
        r["re"] = [re.compile(p, re.IGNORECASE) for p in r["name_patterns"]]
    return rules


def in_bbox(way, bbox):
    for n in way.nodes:
        loc = n.location
        if loc.valid() and bbox[0] <= loc.lon <= bbox[2] and bbox[1] <= loc.lat <= bbox[3]:
            return True
    return False


def names(tags):
    return [v for k, v in tags.items() if k == "name" or k.startswith("name:") or k in ("ref", "official_name", "alt_name")]


def india(way, tags, rules, report):
    changed = False
    hw = tags.get("highway")
    if hw is None:
        return None
    for r in rules:
        if hw not in r["highways"]:
            continue
        ns = names(tags)
        if not any(rx.search(n) for rx in r["re"] for n in ns):
            continue
        if not in_bbox(way, r["bbox"]):
            continue
        tags["motor_vehicle"] = "no"
        tags["motorcar"] = "no"
        tags["pagdandi:override"] = r["id"]
        report.setdefault(r["id"], []).append(way.id)
        changed = True
        break
    if tags.get("motorcycle") == "no" and tags.get("motor_vehicle") != "no":
        tags["motor_vehicle"] = "no"
        tags["motorcar"] = "no"
        report.setdefault("motorcycle_no", []).append(way.id)
        changed = True
    return tags if changed else None


def sweden(way, tags, report):
    if tags.get("highway") != "track":
        return None
    explicit = any(tags.get(k) in MOTOR_OK for k in MOTOR_KEYS)
    if explicit:
        return None
    tags["motor_vehicle"] = "no"
    tags["motorcar"] = "no"
    report.setdefault("se_track_no_motor", []).append(way.id)
    return tags


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--country", required=True, choices=["in", "se"])
    ap.add_argument("--in", dest="inp", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--rules")
    ap.add_argument("--report", required=True)
    a = ap.parse_args()

    rules = load_rules(a.rules) if a.rules else []
    report = {}
    fp = osmium.FileProcessor(a.inp).with_locations()
    w = osmium.SimpleWriter(a.out, overwrite=True)
    n_ways = 0
    for obj in fp:
        if obj.is_way():
            n_ways += 1
            tags = dict(obj.tags)
            new = india(obj, tags, rules, report) if a.country == "in" else sweden(obj, tags, report)
            if new is not None:
                w.add_way(obj.replace(tags=new))
                continue
        w.add(obj)
    w.close()
    summary = {k: len(v) for k, v in report.items()}
    with open(a.report, "w", encoding="utf-8") as f:
        json.dump({"ways_scanned": n_ways, "matched": summary, "way_ids": {k: v[:500] for k, v in report.items()}}, f, indent=1)
    print(json.dumps({"ways_scanned": n_ways, "matched": summary}))


if __name__ == "__main__":
    sys.exit(main())
