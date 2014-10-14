#! /usr/bin/env python3

import json
import glob
import collections
import csv

default = {
    "in-multisig": False,
    "out-multisig": False,
    "balance": 0,
    "out-addr": [],
    "attacker-name": None,
    "last-out-time": None,
    "attacker-time": [],
}

known = {
    "1KtjBE8yDxoqNTSyLG2re4qtKK19KpvVLT": "KJ",
    "1BkE8ttBRUKVNTj3Lx1EPsw7vVbhuLZhBt": "KJ",
    "1GozmcsMBC7bnMVUQLTKEw5vBxbSeG4erW": "GOMEZ",
    "1HKywxiL4JziqXrzLKhmB6a74ma6kxbSDj": "GOMEZ",
}

result = {}
out_addr_last = collections.Counter()
out_addr_all = collections.Counter()

for filename in glob.glob("data/1*.json"):
    with open(filename) as f:
        addr_info = json.load(f)

    r = default.copy()

    if any(inp["prev_out"]["type"] == 1 for tx in addr_info["txs"] for inp in tx["inputs"] if "prev_out" in inp):
        r["in-multisig"] = True

    if any(out["type"] == 1 for tx in addr_info["txs"] for out in tx["out"]):
        r["out-multisig"] = True

    r["balance"] = addr_info["final_balance"]

    out_tx = [tx for tx in addr_info["txs"]
        if any(inp["prev_out"]["addr"] == addr_info["address"] for inp in tx["inputs"] if "prev_out" in inp)]

    # if not r["in-multisig"] and not r["out-multisig"]:
    try:
        r["out-addr"] = [out["addr"] for tx in out_tx for out in tx["out"]
            if out["addr"] != addr_info["address"]]
        out_addr_last.update(r["out-addr"][:1])
        out_addr_all.update(set(r["out-addr"]))
    except:
        pass

    for k in known:
        if k in r["out-addr"]:
            if r["attacker-name"] is not None and r["attacker-name"] != known[k]:
                raise Exception(addr_info["address"])
            r["attacker-name"] = known[k]
            r["attacker-time"] = [tx["time"] for tx in out_tx for out in tx["out"] if out["addr"] == k]

    r["last-out-time"] = out_tx[0]["time"]

    result[addr_info["address"]] = r

with open("data/analyzr.tsv") as f:
    reader = csv.reader(f, delimiter='\t')
    next(reader)
    tx_dict = collections.defaultdict(list)
    for row in reader:
        if len(row) <= 10: continue
        tx_dict[row[4]].append(row[9] + row[10])
    for tx_sha in tuple(tx_dict.keys()):
        if collections.Counter(tx_dict[tx_sha]).most_common()[0][1] > 1:
            result[tx_sha] = {
                "repeated-r": True,
            }

with open('data/tags.json', 'w') as f:
    json.dump(result, f, indent=4)

out_addr_last_X = collections.Counter()
out_addr_all_X = collections.Counter()

print()
for addr, num in out_addr_last.most_common():
    if num > 1:
        print(addr, num)
    if addr.startswith('1dice'): continue
    if addr in known:
        out_addr_last_X[known[addr]] += num
        continue
    out_addr_last_X[addr] += num

print()
for addr, num in out_addr_all.most_common():
    if num > 1:
        print(addr, num)
    if addr.startswith('1dice'): continue
    if addr in known:
        out_addr_all_X[known[addr]] += num
        continue
    out_addr_all_X[addr] += num

print()
for addr, num in out_addr_last_X.most_common():
    if num > 1:
        print(addr, num)

print()
for addr, num in out_addr_all_X.most_common():
    if num > 1:
        print(addr, num)
