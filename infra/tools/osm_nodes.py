"""Export routable-way nodes and barrier nodes as CSV for PostGIS (SPEC §7.1, §4.3).

nodes.csv:    id,lon,lat           every node of a way the country's vehicles can use
barriers.csv: id,kind,access,lon,lat   barrier nodes on those ways

Run with the osmium conda env: python osm_nodes.py --country in --in X.pbf --nodes n.csv --barriers b.csv
"""
import argparse
import sys

import osmium
from osmium.index import IdSet

NOT_ROUTABLE = {"proposed", "construction", "abandoned", "platform", "corridor", "elevator", "raceway", "bus_stop", "razed"}
FOOT_ONLY = {"footway", "path", "steps", "pedestrian", "bridleway", "cycleway"}
ACCESS_KEYS = ("motor_vehicle", "motorcar", "vehicle", "bicycle", "access")


def routable(tags, country):
    hw = tags.get("highway")
    if hw is None or hw in NOT_ROUTABLE:
        return False
    if country == "in" and hw in FOOT_ONLY:
        return False
    if country == "se" and hw == "steps":
        return False
    if tags.get("area") == "yes":
        return False
    return True


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--country", required=True, choices=["in", "se"])
    ap.add_argument("--in", dest="inp", required=True)
    ap.add_argument("--nodes", required=True)
    ap.add_argument("--barriers", required=True)
    a = ap.parse_args()

    # Pass 1: node ids referenced by routable ways.
    wanted = IdSet()
    for w in osmium.FileProcessor(a.inp, osmium.osm.WAY):
        if routable(w.tags, a.country):
            for n in w.nodes:
                wanted.set(n.ref)

    # Pass 2: write their locations, plus barrier tags.
    n_nodes = n_barriers = 0
    with open(a.nodes, "w", encoding="utf-8", newline="") as fn, open(a.barriers, "w", encoding="utf-8", newline="") as fb:
        for n in osmium.FileProcessor(a.inp, osmium.osm.NODE):
            if not wanted.get(n.id) or not n.location.valid():
                continue
            lon, lat = n.location.lon, n.location.lat
            fn.write(f"{n.id},{lon:.7f},{lat:.7f}\n")
            n_nodes += 1
            kind = n.tags.get("barrier")
            if kind:
                access = ""
                for k in ACCESS_KEYS:
                    if k in n.tags:
                        access = n.tags[k].replace(",", ";")
                        break
                fb.write(f"{n.id},{kind.replace(',', ';')},{access},{lon:.7f},{lat:.7f}\n")
                n_barriers += 1
    print(f'{{"nodes": {n_nodes}, "barriers": {n_barriers}}}')


if __name__ == "__main__":
    sys.exit(main())
