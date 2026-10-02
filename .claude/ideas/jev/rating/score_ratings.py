"""Scores Jev's highlight against the human "worth watching" ratings from the
Round Ratings page.

1. Export the ratings: ArtifactData list on the page's "ratings" collection
   with out_dir=<dir>, which writes <dir>/ratings/<key>.json.
2. python score_ratings.py <dir>/ratings

rounds.json maps each page key (R01..R40) to its source round, Jev's highlight
score (0-4) and, for public rounds, the E2 rubric. The ship bar in the Jev
spec is Spearman rho >= 0.6 between Jev and the human ratings.
"""
import glob
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def ranks(xs):
    order = sorted(range(len(xs)), key=lambda i: xs[i])
    r, i = [0.0] * len(xs), 0
    while i < len(order):
        j = i
        while j + 1 < len(order) and xs[order[j + 1]] == xs[order[i]]:
            j += 1
        for k in range(i, j + 1):
            r[order[k]] = (i + j) / 2
        i = j + 1
    return r


def spearman(a, b):
    ra, rb = ranks(a), ranks(b)
    ma, mb = sum(ra) / len(ra), sum(rb) / len(rb)
    cov = sum((x - ma) * (y - mb) for x, y in zip(ra, rb))
    return cov / (sum((x - ma) ** 2 for x in ra) * sum((y - mb) ** 2 for y in rb)) ** 0.5


def main():
    rounds = json.load(open(os.path.join(HERE, "rounds.json")))
    human = {}
    for f in glob.glob(os.path.join(sys.argv[1], "*.json")):
        d = json.load(open(f))
        body = d.get("data", d)
        if body.get("rating"):
            human[os.path.basename(f)[:-5]] = body["rating"]
    keys = sorted(k for k in human if k in rounds)
    if len(keys) < 5:
        sys.exit(f"only {len(keys)} rated rounds; rate more first")
    h = [human[k] for k in keys]
    j = [rounds[k]["jev_highlight"] for k in keys]
    print(f"{len(keys)} rated rounds. Spearman(Jev highlight, human): {spearman(j, h):.2f}  (ship bar: 0.6)")
    pub = [k for k in keys if rounds[k].get("rubric") is not None]
    if len(pub) >= 5:
        print(f"public rounds only ({len(pub)}): Jev vs human {spearman([rounds[k]['jev_highlight'] for k in pub], [human[k] for k in pub]):.2f},"
              f" rubric vs human {spearman([rounds[k]['rubric'] for k in pub], [human[k] for k in pub]):.2f}")
    top_h = set(sorted(keys, key=lambda k: -human[k])[:5])
    top_j = set(sorted(keys, key=lambda k: -rounds[k]["jev_highlight"])[:5])
    print(f"top-5 overlap (Jev vs human): {len(top_h & top_j)} of 5")


if __name__ == "__main__":
    main()
