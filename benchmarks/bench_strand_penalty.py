#!/usr/bin/env python3
"""Continuous vs threshold strand penalty — asked by @Whatsonyourmind (Apr)
and re-asked by @AutuSnow (May) on koordinator-sh/koordinator#2837.

Success criterion, per Whatsonyourmind: the threshold version should produce a
bimodal / degenerate score distribution where top candidates tie; the continuous
version should spread them so the scheduler can differentiate."""
import random, statistics as st
src = open("benchmark_simulation.py").read().replace('if __name__ == "__main__":','if False:')
bs = {}; exec(compile(src,"b","exec"), bs)
N_DIMS, cosine_sim, SCENARIOS = bs["N_DIMS"], bs["cosine_sim"], bs["SCENARIOS"]

def score(node, pod, continuous):
    if not node.can_fit(pod.req): return -1
    act=[i for i in range(N_DIMS) if node.capacity[i]>0]
    after=[(node.used[i]+pod.req[i])/node.capacity[i] for i in act]
    m=sum(after)/len(after); var=sum((x-m)**2 for x in after)/len(after)
    v=max(0,(1.0-var*4)*100)
    ff=node.free_frac(); nf=[ff[i] for i in act]
    pf=[pod.req[i]/node.capacity[i] if node.capacity[i]>0 else 0 for i in act]
    a=cosine_sim(nf,pf)*100; h=(sum(nf)/len(nf))*100
    p=0
    for u in after:
        if u>0.92: p+=(u-0.92)*500
        elif u>0.85: p+=(u-0.85)*50
    s=0
    for i in range(len(after)):
        for j in range(i+1,len(after)):
            hi,lo=max(after[i],after[j]),min(after[i],after[j])
            if continuous:
                # smooth: penalty grows with the spread, no cliff
                s += 15*max(0.0,(hi-lo)-0.30)/0.70
            else:
                if hi>0.80 and lo<0.20: s+=15
    return max(0,min(100,0.6*v+0.2*a+0.1*h-p-s))

print("TOP-5 CANDIDATE SCORE SPREAD  (higher spread = finer differentiation)\n")
print(f"  {'scenario':<26}{'threshold':>22}{'continuous':>22}")
print(f"  {'':<26}{'spread':>11}{'ties':>11}{'spread':>11}{'ties':>11}")
print("  "+"-"*70)
tot={'t':[],'c':[],'tt':0,'ct':0}
for sc in SCENARIOS:
    for mode,key in ((False,'t'),(True,'c')):
        random.seed(42)
        nodes=sc["cluster"](); pods=bs["gen_workload"](sc["n"],sc["mix"])
        random.shuffle(pods)
        spreads=[]; ties=0
        for pod in pods:
            cand=sorted([score(n,pod,mode) for n in nodes if n.can_fit(pod.req)],reverse=True)[:5]
            if len(cand)>=2:
                spreads.append(cand[0]-cand[-1])
                if abs(cand[0]-cand[1])<1e-9: ties+=1
            # place greedily so the cluster evolves
            best=None;bs_=-2
            for n in nodes:
                s_=score(n,pod,mode)
                if s_>bs_: bs_,best=s_,n
            if best and best.can_fit(pod.req): best.place(pod.req)
        avg=sum(spreads)/len(spreads) if spreads else 0
        tot[key].append(avg); tot[key+'t' if False else ('tt' if key=='t' else 'ct')]+=ties
        if key=='t': t_avg,t_ties=avg,ties
        else: c_avg,c_ties=avg,ties
    print(f"  {sc['name'][:25]:<26}{t_avg:>11.2f}{t_ties:>11}{c_avg:>11.2f}{c_ties:>11}")
print("  "+"-"*70)
print(f"  {'MEAN':<26}{sum(tot['t'])/len(tot['t']):>11.2f}{tot['tt']:>11}"
      f"{sum(tot['c'])/len(tot['c']):>11.2f}{tot['ct']:>11}")
